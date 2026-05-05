package modules_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestSeedListsUnavailableModulesAndRestartsAvailableModule(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	store := modules.NewStore(handle)
	if err := store.Seed(ctx); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	states, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}
	if len(states) != len(modules.InitialRegistry()) {
		t.Fatalf("states = %d, want %d", len(states), len(modules.InitialRegistry()))
	}
	foundUnavailable := false
	for _, state := range states {
		if state.ModuleName == "global-scanner" && state.State == modules.StateUnavailable {
			foundUnavailable = true
		}
	}
	if !foundUnavailable {
		t.Fatal("expected global-scanner to be unavailable in Stage 02")
	}
	if _, err := store.Restart(ctx, "global-scanner", "test"); err == nil {
		t.Fatal("expected unavailable module restart to fail")
	}
	result, err := store.Restart(ctx, "config-manager", "test")
	if err != nil {
		t.Fatalf("restart config-manager: %v", err)
	}
	if result.NewState != modules.StateRunning || result.SchemaVersion != "module_restart.result.v1" {
		t.Fatalf("unexpected restart result: %+v", result)
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
