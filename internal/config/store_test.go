package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestStoreImportPersistsConfigAndMasksSecrets(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	file, err := os.Open("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		t.Fatalf("decode example: %v", err)
	}
	store := appconfig.NewStore(handle)
	result, err := store.Import(ctx, cfg, []string{".terraform/", "!keep"}, "test")
	if err != nil {
		t.Fatalf("import config: %v", err)
	}
	if len(result.AppliedKeys) == 0 {
		t.Fatal("expected applied reloadable keys")
	}

	active, err := store.ActiveConfig(ctx)
	if err != nil {
		t.Fatalf("active config: %v", err)
	}
	external := active["external_databases"].(map[string]any)
	password := external["password"].(map[string]any)
	if password["masked"] != true || password["ref_type"] != "env" {
		t.Fatalf("password not masked: %#v", password)
	}

	var rules int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM ignore_rules WHERE origin = 'config_import'").Scan(&rules); err != nil {
		t.Fatal(err)
	}
	if rules != 2 {
		t.Fatalf("ignore rules = %d, want 2", rules)
	}
}

func openMigratedSQLite(t *testing.T) *storage.Handle {
	t.Helper()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(context.Background(), storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "stage02.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := provider.Migrate(context.Background(), handle); err != nil {
		handle.Close()
		t.Fatalf("migrate sqlite: %v", err)
	}
	return handle
}
