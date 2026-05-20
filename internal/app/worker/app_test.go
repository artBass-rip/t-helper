package worker

import (
	"context"
	"encoding/json"
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

func TestSQLiteWorkerProcessLimitLockRejectsSecondActiveWorker(t *testing.T) {
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
	app := New(Config{WorkerLockDir: t.TempDir()}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	opts, err := app.runtimeOptions(ctx, handle.Provider, configStore, jobs.NewStore(handle), moduleStore)
	if err != nil {
		t.Fatalf("runtime options: %v", err)
	}
	first, err := app.acquireProcessLimitLock(ctx, handle, opts.WorkerID, configStore)
	if err != nil {
		t.Fatalf("acquire first worker lock: %v", err)
	}
	if _, err := app.acquireProcessLimitLock(ctx, handle, "host:2:worker_conflict", configStore); err == nil {
		t.Fatal("expected second active SQLite worker to be rejected")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first worker lock: %v", err)
	}
	second, err := app.acquireProcessLimitLock(ctx, handle, "host:2:worker_next", configStore)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second worker lock: %v", err)
	}
}

func TestNonSQLiteWorkerProcessLimitLockIsNotRequired(t *testing.T) {
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
	handle.Provider = "postgres"

	configStore := appconfig.NewStore(handle)
	app := New(Config{WorkerLockDir: t.TempDir()}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	lock, err := app.acquireProcessLimitLock(ctx, handle, "host:1:worker", configStore)
	if err != nil {
		t.Fatalf("acquire non-sqlite worker lock: %v", err)
	}
	if lock != nil {
		t.Fatalf("non-sqlite worker should not acquire local process lock: %+v", lock)
	}
}

func TestWorkerProcessLockReplacesStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.lock")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create stale lock: %v", err)
	}
	if err := json.NewEncoder(file).Encode(workerLockMetadata{
		SchemaVersion:       workerLockSchemaVersion,
		PID:                 -1,
		WorkerID:            "host:1:stale",
		DatabaseFingerprint: "db_stale",
		StartedAt:           time.Now().UTC().Add(-time.Hour),
		UpdatedAt:           time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close stale lock: %v", err)
	}
	lock, err := acquireWorkerProcessLock(path, workerLockMetadata{
		WorkerID:            "host:2:fresh",
		DatabaseFingerprint: "db_stale",
	})
	if err != nil {
		t.Fatalf("acquire after stale lock: %v", err)
	}
	defer lock.Release()
	metadata, err := readWorkerProcessLock(path)
	if err != nil {
		t.Fatalf("read replaced lock: %v", err)
	}
	if metadata.WorkerID != "host:2:fresh" || metadata.PID != os.Getpid() {
		t.Fatalf("stale worker lock was not replaced: %+v", metadata)
	}
}

func TestWorkerRunExitsWithoutClaimingJobsWhenDisabled(t *testing.T) {
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "worker-disabled.db")
	handle, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := loadExampleConfig(t)
	cfg.Database.DatabasePath = dbPath
	cfg.Workers.Enabled = false
	if _, err := appconfig.NewStore(handle).Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("import config: %v", err)
	}
	store := jobs.NewStore(handle)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	app := New(Config{
		StorageProvider: "sqlite",
		StorageDSN:      dbPath,
		WorkerLockDir:   t.TempDir(),
	}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := app.Run(ctx); err != nil {
		t.Fatalf("worker run: %v", err)
	}
	job, err := store.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != jobs.StatusQueued || job.LeasedBy != "" || job.AttemptCount != 0 {
		t.Fatalf("disabled worker claimed job: %+v", job)
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
	targetStore := jobs.NewStore(target)
	reloadRef, err := targetStore.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: []byte(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue target reload job: %v", err)
	}
	restartRef, err := targetStore.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "module_restart",
		Payload: []byte(`{"schema_version":"jobs.module_restart.payload.v1","module_name":"core"}`),
	})
	if err != nil {
		t.Fatalf("enqueue target restart job: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	app := New(Config{
		StorageProvider:   "sqlite",
		StorageDSN:        sourcePath,
		PollInterval:      5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
		WorkerLockDir:     t.TempDir(),
	}, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(runCtx)
	}()

	deadline := time.After(2 * time.Second)
	for {
		reloadJob, err := targetStore.Get(ctx, reloadRef.JobID)
		if err != nil {
			t.Fatalf("get target reload job: %v", err)
		}
		restartJob, err := targetStore.Get(ctx, restartRef.JobID)
		if err != nil {
			t.Fatalf("get target restart job: %v", err)
		}
		if reloadJob.Status == jobs.StatusSucceeded && restartJob.Status == jobs.StatusSucceeded {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("target jobs were not processed by worker: reload=%+v restart=%+v", reloadJob, restartJob)
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

	if _, err := jobs.NewStore(source).Get(ctx, reloadRef.JobID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("reload job should belong to active target database, source lookup err=%v", err)
	}
	if _, err := jobs.NewStore(source).Get(ctx, restartRef.JobID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("restart job should belong to active target database, source lookup err=%v", err)
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
