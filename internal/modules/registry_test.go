package modules_test

import (
	"context"
	"path/filepath"
	"testing"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestConfigInitialModuleNamesMatchRuntimeRegistry(t *testing.T) {
	configNames := appconfig.InitialModuleNames()
	for _, def := range modules.InitialRegistry() {
		if _, ok := configNames[def.Name]; !ok {
			t.Fatalf("runtime module %q is missing from config validation", def.Name)
		}
		delete(configNames, def.Name)
	}
	if len(configNames) != 0 {
		t.Fatalf("config validation has modules missing from runtime registry: %v", configNames)
	}
}

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
		if state.ModuleName == "project-scanner" && state.State == modules.StateUnavailable {
			foundUnavailable = true
		}
	}
	if !foundUnavailable {
		t.Fatal("expected project-scanner to remain unavailable before Stage 06")
	}
	if _, err := store.Restart(ctx, "project-scanner", "test"); err == nil {
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

func TestSeedUsesEnabledModulesAndReloadsAvailableModule(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	store := modules.NewStore(handle)
	if err := store.Seed(ctx, []string{"core"}); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	states, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}
	var core, configManager modules.ModuleState
	for _, state := range states {
		switch state.ModuleName {
		case "core":
			core = state
		case "config-manager":
			configManager = state
		}
	}
	if core.State != modules.StateRunning {
		t.Fatalf("core state = %q, want running", core.State)
	}
	if configManager.State != modules.StateStopped {
		t.Fatalf("config-manager state = %q, want stopped", configManager.State)
	}
	if _, err := store.Reload(ctx, "project-scanner", "test"); err == nil {
		t.Fatal("expected unavailable module reload to fail")
	}
	if _, err := store.Reload(ctx, "core", "test"); err != nil {
		t.Fatalf("reload core: %v", err)
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
