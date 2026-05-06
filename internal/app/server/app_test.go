package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestAppHealthSmoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry := storageproviders.MVPRegistry()
	cfg := DefaultConfig()
	cfg.StorageDSN = filepath.Join(t.TempDir(), "stage01-smoke.db")
	app := New(cfg, registry, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handle, err := registry.Open(ctx, storage.Config{Provider: cfg.StorageProvider, DSN: cfg.StorageDSN})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	handler, err := app.BuildHandler(ctx, handle)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.ServeListener(ctx, listener, handler)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("server shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("server did not stop")
		}
	})

	var response *http.Response
	for i := 0; i < 50; i++ {
		response, err = http.Get("http://" + address + "/api/health")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}

	var status runtime.HealthStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode health status: %v", err)
	}
	if status.SchemaVersion != runtime.HealthSchemaVersion || status.Readiness != runtime.ReadinessReady {
		t.Fatalf("unexpected health status: %+v", status)
	}
	if status.InstanceID == "" || status.DatabaseFingerprint == "" || status.StartedAt.IsZero() {
		t.Fatalf("missing required health fields: %+v", status)
	}
}

func TestAppResolvesPromotedCurrentStorageProfile(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	targetPath := filepath.Join(dir, "target.db")

	source, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: sourcePath})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer source.Close()
	if err := registry.Migrate(ctx, source); err != nil {
		t.Fatalf("migrate source: %v", err)
	}
	store := appconfig.NewStore(source)
	cfg := loadServerTestConfig(t)
	cfg.Database.DatabasePath = sourcePath
	if _, err := store.Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	cfg.Database.DatabasePath = targetPath
	if _, err := store.Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("target import: %v", err)
	}
	if _, err := store.MigrateDB(ctx, registry); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	app := New(DefaultConfig(), registry, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	current, err := appconfig.NewStore(source).CurrentStorageProfile(ctx)
	if err != nil {
		t.Fatalf("current profile: %v", err)
	}
	resolved, err := app.resolveCurrentProfileHandle(ctx, source)
	if err != nil {
		t.Fatalf("resolve current profile: %v", err)
	}
	defer resolved.Close()
	if resolved.Fingerprint == source.Fingerprint {
		t.Fatal("expected runtime to resolve promoted target handle")
	}
	if resolved.Fingerprint != current.DatabaseFingerprint {
		t.Fatalf("resolved fingerprint = %q, want %q", resolved.Fingerprint, current.DatabaseFingerprint)
	}
}

func TestAppReturnsCurrentStorageProfileReadErrors(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	handle, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "closed.db")})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close storage: %v", err)
	}

	app := New(DefaultConfig(), registry, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := app.resolveCurrentProfileHandle(ctx, handle); err == nil {
		t.Fatal("expected current profile read error")
	}
}

func TestAppAppliesPersistedRuntimeConfigAndModuleEnablement(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	dbPath := filepath.Join(t.TempDir(), "runtime-config.db")
	handle, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	cfg := loadServerTestConfig(t)
	cfg.Database.DatabasePath = dbPath
	cfg.API.ListenAddress = "127.0.0.1:18081"
	cfg.SystemSettings.Mode = "server"
	cfg.Logging.Level = "debug"
	cfg.Modules.Enabled = []string{"core"}
	if _, err := appconfig.NewStore(handle).Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("import config: %v", err)
	}

	appCfg := DefaultConfig()
	appCfg.ListenAddress = "127.0.0.1:9999"
	appCfg.Mode = "local"
	appCfg.LogLevel = "info"
	app := New(appCfg, registry, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := app.applyPersistedRuntimeConfig(ctx, handle); err != nil {
		t.Fatalf("apply runtime config: %v", err)
	}
	if app.cfg.ListenAddress != "127.0.0.1:18081" || app.cfg.Mode != "server" || app.cfg.LogLevel != "debug" {
		t.Fatalf("runtime config not applied: %+v", app.cfg)
	}
	if _, err := app.BuildHandler(ctx, handle, "runtime_test"); err != nil {
		t.Fatalf("build handler: %v", err)
	}
	states, err := modules.NewStore(handle).List(ctx)
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}
	for _, state := range states {
		if state.ModuleName == "config-manager" && state.State != modules.StateStopped {
			t.Fatalf("config-manager state = %q, want stopped", state.State)
		}
	}
}

func loadServerTestConfig(t *testing.T) appconfig.RuntimeConfig {
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
	return cfg
}
