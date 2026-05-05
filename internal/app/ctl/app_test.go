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
	if reload.SchemaVersion != "config_reload.result.v1" || len(reload.AppliedKeys) == 0 || len(reload.FailedKeys) != 0 {
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
}
