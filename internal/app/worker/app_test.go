package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestRuntimeOptionsUseProviderWorkerSettings(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	dbPath := filepath.Join(t.TempDir(), "worker.db")
	handle, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	configStore := appconfig.NewStore(handle)
	file, err := os.Open("../../../config.example.json")
	if err != nil {
		t.Fatalf("open example config: %v", err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		t.Fatalf("decode example config: %v", err)
	}
	cfg.Database.DatabasePath = dbPath
	if _, err := configStore.Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("import config: %v", err)
	}
	moduleStore := modules.NewStore(handle)
	app := New(Config{PollInterval: time.Millisecond}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))

	opts, err := app.runtimeOptions(ctx, handle.Provider, configStore, jobs.NewStore(handle), moduleStore)
	if err != nil {
		t.Fatalf("runtime options: %v", err)
	}
	if opts.Concurrency != 1 || opts.LeaseDuration != 30*time.Second || opts.HeartbeatInterval != 10*time.Second {
		t.Fatalf("runtime options did not use provider settings: %+v", opts)
	}
}

func TestRuntimeOptionsRejectSQLiteConcurrencyOverride(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	handle, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "worker.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	configStore := appconfig.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	app := New(Config{Concurrency: 2}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := app.runtimeOptions(ctx, handle.Provider, configStore, jobs.NewStore(handle), moduleStore); err == nil {
		t.Fatal("expected sqlite_worker_concurrency_unsupported")
	}
}

func TestWorkerRunConsumesJobsFromActiveStorageProfile(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	targetPath := filepath.Join(dir, "target.db")
	source, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: sourcePath})
	if err != nil {
		t.Fatalf("open source sqlite: %v", err)
	}
	defer source.Close()
	if err := registry.Migrate(ctx, source); err != nil {
		t.Fatalf("migrate source: %v", err)
	}

	sourceConfig := appconfig.NewStore(source)
	cfg := loadExampleConfig(t)
	cfg.Database.DatabasePath = sourcePath
	if _, err := sourceConfig.Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("import source config: %v", err)
	}
	next := cfg
	next.Database.DatabasePath = targetPath
	if _, err := sourceConfig.Import(ctx, next, nil, "test"); err != nil {
		t.Fatalf("stage target config: %v", err)
	}
	if _, err := sourceConfig.MigrateDB(ctx, registry); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	target, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: targetPath})
	if err != nil {
		t.Fatalf("open target sqlite: %v", err)
	}
	defer target.Close()
	if err := registry.Migrate(ctx, target); err != nil {
		t.Fatalf("migrate target: %v", err)
	}
	ref, err := jobs.NewStore(target).Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: []byte(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue target job: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	app := New(Config{
		StorageProvider:   "sqlite",
		StorageDSN:        sourcePath,
		PollInterval:      5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
	}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(runCtx)
	}()

	targetJobs := jobs.NewStore(target)
	deadline := time.After(2 * time.Second)
	for {
		job, err := targetJobs.Get(ctx, ref.JobID)
		if err != nil {
			t.Fatalf("get target job: %v", err)
		}
		if job.Status == jobs.StatusSucceeded {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("target job was not processed by worker: %+v", job)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("worker run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}

	if _, err := jobs.NewStore(source).Get(ctx, ref.JobID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("job should belong to active target database, source lookup err=%v", err)
	}
}

func loadExampleConfig(t *testing.T) appconfig.RuntimeConfig {
	t.Helper()
	file, err := os.Open("../../../config.example.json")
	if err != nil {
		t.Fatalf("open example config: %v", err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		t.Fatalf("decode example config: %v", err)
	}
	return cfg
}
