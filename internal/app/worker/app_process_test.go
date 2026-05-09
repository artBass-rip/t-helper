package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestWorkerProcessConsumesJobsFromSeparateProcess(t *testing.T) {
	if os.Getenv("THELPER_WORKER_PROCESS_HELPER") == "1" {
		runWorkerProcessHelper(t)
		return
	}

	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "worker-process.db")
	lockDir := filepath.Join(dir, "locks")
	handle, err := registry.Open(ctx, storage.Config{Provider: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	cfg := loadExampleConfig(t)
	cfg.Database.DatabasePath = dbPath
	if _, err := appconfig.NewStore(handle).Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("import config: %v", err)
	}

	store := jobs.NewStore(handle)
	reloadRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue reload job: %v", err)
	}
	restartRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "module_restart",
		Payload: json.RawMessage(`{"schema_version":"jobs.module_restart.payload.v1","module_name":"core"}`),
	})
	if err != nil {
		t.Fatalf("enqueue restart job: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerProcessConsumesJobsFromSeparateProcess$", "-test.v")
	cmd.Env = append(os.Environ(),
		"THELPER_WORKER_PROCESS_HELPER=1",
		"THELPER_WORKER_PROCESS_DSN="+dbPath,
		"THELPER_WORKER_PROCESS_LOCK_DIR="+lockDir,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker helper process: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-waitCh
		}
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case err := <-waitCh:
			t.Fatalf("worker helper exited before processing jobs: err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		default:
		}
		reloadJob, err := store.Get(ctx, reloadRef.JobID)
		if err != nil {
			t.Fatalf("get reload job: %v", err)
		}
		restartJob, err := store.Get(ctx, restartRef.JobID)
		if err != nil {
			t.Fatalf("get restart job: %v", err)
		}
		if reloadJob.Status == jobs.StatusSucceeded && restartJob.Status == jobs.StatusSucceeded {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("worker helper did not process jobs: reload=%+v restart=%+v stdout=%s stderr=%s", reloadJob, restartJob, stdout.String(), stderr.String())
		case <-time.After(20 * time.Millisecond):
		}
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("interrupt worker helper: %v", err)
	}
	select {
	case err := <-waitCh:
		if err != nil {
			t.Fatalf("worker helper did not exit cleanly: err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("worker helper did not stop after interrupt")
	}
}

func runWorkerProcessHelper(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("THELPER_WORKER_PROCESS_DSN")
	if dsn == "" {
		t.Fatal("THELPER_WORKER_PROCESS_DSN is required")
	}
	lockDir := os.Getenv("THELPER_WORKER_PROCESS_LOCK_DIR")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	app := New(Config{
		StorageProvider:   "sqlite",
		StorageDSN:        dsn,
		PollInterval:      5 * time.Millisecond,
		HeartbeatInterval: 5 * time.Millisecond,
		WorkerLockDir:     lockDir,
	}, storageproviders.MVPRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("worker helper run: %v", err)
	}
}
