package jobs_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestJobStoreContractSQLite(t *testing.T) {
	store := openStore(t)
	runJobStoreContract(t, store)
}

func TestJobStoreContractPostgres(t *testing.T) {
	dsn := os.Getenv("THELPER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("THELPER_POSTGRES_DSN is not set")
	}
	requirePostgresTestDatabase(t, dsn)
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	handle, err := registry.Open(ctx, storage.Config{Provider: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer handle.Close()
	resetStage03Tables(t, handle.DB)
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}
	runJobStoreContract(t, jobs.NewStore(handle))
}

func runJobStoreContract(t *testing.T, store *jobs.Store) {
	t.Helper()
	ctx := context.Background()
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var claims atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Minute})
			if err != nil {
				t.Errorf("claim worker %d: %v", i, err)
				return
			}
			if ok {
				if claimed.ID != ref.JobID {
					t.Errorf("claimed wrong job: %+v", claimed)
				}
				claims.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claims = %d, want 1", claims.Load())
	}
	claimed, err := store.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get claimed job: %v", err)
	}
	if err := store.Heartbeat(ctx, claimed.ID, "host:1:worker", time.Minute); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if err := store.Complete(ctx, claimed, "host:1:worker", jobs.StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	workflow, err := store.WorkflowStatus(ctx, "config_operation:"+ref.JobID)
	if err != nil {
		t.Fatalf("workflow status: %v", err)
	}
	if workflow.AggregateStatus != jobs.StatusSucceeded || workflow.ProgressCurrent != 1 || workflow.ProgressTotal != 1 {
		t.Fatalf("unexpected workflow status: %+v", workflow)
	}

	firstRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		LockKey: "resource:contract",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue first locked job: %v", err)
	}
	secondRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		LockKey: "resource:contract",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue second locked job: %v", err)
	}
	first, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:first", LeaseDuration: time.Minute})
	if err != nil || !ok || first.ID != firstRef.JobID {
		t.Fatalf("claim first locked job: ok=%v job=%+v err=%v", ok, first, err)
	}
	locked, err := store.AcquireLock(ctx, first, "host:1:first", time.Minute)
	if err != nil || !locked {
		t.Fatalf("acquire first lock: locked=%v err=%v", locked, err)
	}
	second, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:second", LeaseDuration: time.Minute})
	if err != nil || !ok || second.ID != secondRef.JobID {
		t.Fatalf("claim second locked job: ok=%v job=%+v err=%v", ok, second, err)
	}
	locked, err = store.AcquireLock(ctx, second, "host:1:second", time.Minute)
	if err != nil {
		t.Fatalf("acquire second lock: %v", err)
	}
	if locked {
		t.Fatal("second worker acquired conflicting held lock")
	}
	if err := store.Requeue(ctx, second, "host:1:second", "lock_contention", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("requeue second locked job: %v", err)
	}
	if err := store.Complete(ctx, first, "host:1:first", jobs.StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), ""); err != nil {
		t.Fatalf("complete first locked job: %v", err)
	}
	requeued, err := store.Get(ctx, secondRef.JobID)
	if err != nil {
		t.Fatalf("get requeued locked job: %v", err)
	}
	if requeued.Status != jobs.StatusQueued || requeued.LeasedBy != "" {
		t.Fatalf("unexpected requeued locked job: %+v", requeued)
	}

	expiredRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:     "config_reload",
		MaxAttempts: 2,
		Payload:     json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue expired job: %v", err)
	}
	expired, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:expired", LeaseDuration: time.Millisecond})
	if err != nil || !ok || expired.ID != expiredRef.JobID {
		t.Fatalf("claim expired job: ok=%v job=%+v err=%v", ok, expired, err)
	}
	time.Sleep(5 * time.Millisecond)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: store, WorkerID: "host:1:recovery", LeaseDuration: time.Minute})
	if err := runtime.RecoverExpiredLeases(ctx); err != nil {
		t.Fatalf("recover expired leases: %v", err)
	}
	recovered, err := store.Get(ctx, expiredRef.JobID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if recovered.Status != jobs.StatusQueued || recovered.LeasedBy != "" || recovered.AttemptCount != 1 {
		t.Fatalf("unexpected recovered job: %+v", recovered)
	}
}

func requirePostgresTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse THELPER_POSTGRES_DSN: %v", err)
	}
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if strings.HasSuffix(dbName, "_test") || strings.Contains(dbName, "test") {
		return
	}
	if os.Getenv("THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS") == "1" {
		return
	}
	t.Fatalf("refusing destructive jobs contract test against database %q; use a test database or set THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS=1", dbName)
}

func resetStage03Tables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		"DROP TABLE IF EXISTS project_links CASCADE",
		"DROP TABLE IF EXISTS workspaces CASCADE",
		"DROP TABLE IF EXISTS projects CASCADE",
		"DROP TABLE IF EXISTS repositories CASCADE",
		"DROP TABLE IF EXISTS environments CASCADE",
		"DROP TABLE IF EXISTS root_paths CASCADE",
		"DROP TABLE IF EXISTS workflow_statuses CASCADE",
		"DROP TABLE IF EXISTS job_events CASCADE",
		"DROP TABLE IF EXISTS job_locks CASCADE",
		"DROP TABLE IF EXISTS jobs CASCADE",
		"DROP TABLE IF EXISTS ignore_rules CASCADE",
		"DROP TABLE IF EXISTS module_states CASCADE",
		"DROP TABLE IF EXISTS storage_provider_settings CASCADE",
		"DROP TABLE IF EXISTS storage_profiles CASCADE",
		"DROP TABLE IF EXISTS config_entries CASCADE",
		"DROP TABLE IF EXISTS system_metadata CASCADE",
		"DROP TABLE IF EXISTS goose_db_version CASCADE",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("reset table with %q: %v", stmt, err)
		}
	}
}
