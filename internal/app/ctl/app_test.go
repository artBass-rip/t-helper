package ctl

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestProvidersListsMVPAdapters(t *testing.T) {
	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.RunCommand(context.Background(), "providers"); err != nil {
		t.Fatalf("providers: %v", err)
	}

	got := strings.Fields(out.String())
	want := []string{"postgres", "sqlite"}
	if len(got) != len(want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("providers = %v, want %v", got, want)
		}
	}
}

func TestRunCommandRejectsUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.RunCommand(context.Background(), "unknown"); err == nil {
		t.Fatal("expected error")
	}
}

func TestStage02CLIReconfigureReloadAndRestartFlow(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "stage02-cli.db")
	ignorePath := filepath.Join(t.TempDir(), ".t-helper.ignore")
	if err := os.WriteFile(ignorePath, []byte(".terraform/\n!keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.Run(ctx, Command{
		Name:            "reconfigure",
		StorageProvider: "sqlite",
		StorageDSN:      dbPath,
		ConfigPath:      "../../../config.example.json",
		IgnorePath:      ignorePath,
	}); err != nil {
		t.Fatalf("reconfigure: %v", err)
	}
	var importResult appconfig.ImportResult
	if err := json.Unmarshal(out.Bytes(), &importResult); err != nil {
		t.Fatalf("decode reconfigure output: %v", err)
	}
	if importResult.SchemaVersion != "config_import.result.v1" || len(importResult.IgnoreRules) != 2 {
		t.Fatalf("unexpected import result: %+v", importResult)
	}

	out.Reset()
	if err := app.Run(ctx, Command{Name: "reload", StorageProvider: "sqlite", StorageDSN: dbPath}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	var reload appconfig.ReloadResult
	if err := json.Unmarshal(out.Bytes(), &reload); err != nil {
		t.Fatalf("decode reload output: %v", err)
	}
	if reload.SchemaVersion != "config_reload.result.v1" || len(reload.AcceptedKeys) == 0 || len(reload.AppliedKeys) != 1 || reload.AppliedKeys[0] != "modules.enabled" || len(reload.FailedKeys) != 0 {
		t.Fatalf("unexpected reload result: %+v", reload)
	}

	out.Reset()
	if err := app.Run(ctx, Command{Name: "restart", StorageProvider: "sqlite", StorageDSN: dbPath, ModuleName: "config-manager"}); err != nil {
		t.Fatalf("restart config-manager: %v", err)
	}
	var restart modules.RestartResult
	if err := json.Unmarshal(out.Bytes(), &restart); err != nil {
		t.Fatalf("decode restart output: %v", err)
	}
	if restart.SchemaVersion != "module_restart.result.v1" || restart.NewState != modules.StateRunning {
		t.Fatalf("unexpected restart result: %+v", restart)
	}

	out.Reset()
	if err := app.Run(ctx, Command{Name: "restart", StorageProvider: "sqlite", StorageDSN: dbPath, ModuleName: "global-scanner"}); err == nil {
		t.Fatal("expected unavailable module restart to fail")
	}

	handle, err := storageproviders.MVPRegistry().Open(ctx, storage.Config{Provider: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("open sqlite after cli lifecycle: %v", err)
	}
	defer handle.Close()
	var jobCount int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM jobs").Scan(&jobCount); err != nil {
		t.Fatalf("count jobs after cli lifecycle: %v", err)
	}
	if jobCount != 0 {
		t.Fatalf("Stage 02 synchronous CLI lifecycle commands created %d jobs, want 0", jobCount)
	}
}

func TestStage02CLIReconfigureRejectsSensitiveLiteralsWhenExternalDatabaseDisabled(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "stage02-cli.db")
	file, err := os.Open("../../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := appconfig.Decode(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExternalDatabase.Enabled = false
	cfg.ExternalDatabase.Username = "admin"
	cfg.ExternalDatabase.Password = "secret"
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "literal-secrets.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.Run(ctx, Command{Name: "reconfigure", StorageProvider: "sqlite", StorageDSN: dbPath, ConfigPath: configPath}); err == nil {
		t.Fatal("expected sensitive literal reconfigure to fail")
	}
}

func TestStage02CLIReconfigureRejectsTrailingConfigPayload(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "stage02-cli.db")
	data, err := os.ReadFile("../../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "trailing.json")
	if err := os.WriteFile(configPath, append(data, []byte(` {}`)...), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.Run(ctx, Command{Name: "reconfigure", StorageProvider: "sqlite", StorageDSN: dbPath, ConfigPath: configPath}); err == nil {
		t.Fatal("expected trailing config payload to fail")
	}
}

func TestStage02CLIMigrateDBPromotesMigrationTarget(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	targetPath := filepath.Join(dir, "target.db")
	currentConfig := writeConfig(t, dir, "current.json", sourcePath)
	targetConfig := writeConfig(t, dir, "target.json", targetPath)

	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.Run(ctx, Command{Name: "reconfigure", StorageProvider: "sqlite", StorageDSN: sourcePath, ConfigPath: currentConfig}); err != nil {
		t.Fatalf("initial reconfigure: %v", err)
	}
	out.Reset()
	if err := app.Run(ctx, Command{Name: "reconfigure", StorageProvider: "sqlite", StorageDSN: sourcePath, ConfigPath: targetConfig}); err != nil {
		t.Fatalf("target reconfigure: %v", err)
	}
	out.Reset()
	if err := app.Run(ctx, Command{Name: "migrate-db", StorageProvider: "sqlite", StorageDSN: sourcePath}); err != nil {
		t.Fatalf("migrate-db: %v", err)
	}
	var result appconfig.MigrationResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode migrate output: %v", err)
	}
	if result.Status != "migration_succeeded" || result.CurrentProfileUnchanged {
		t.Fatalf("unexpected migrate result: %+v", result)
	}
}

func writeConfig(t *testing.T, dir, name, dbPath string) string {
	t.Helper()
	file, err := os.Open("../../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.DatabasePath = dbPath
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
