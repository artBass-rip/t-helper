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
		"DROP TABLE IF EXISTS workflow_statuses",
		"DROP TABLE IF EXISTS job_events",
		"DROP TABLE IF EXISTS job_locks",
		"DROP TABLE IF EXISTS jobs",
		"DROP TABLE IF EXISTS ignore_rules",
		"DROP TABLE IF EXISTS module_states",
		"DROP TABLE IF EXISTS storage_provider_settings",
		"DROP TABLE IF EXISTS storage_profiles",
		"DROP TABLE IF EXISTS config_entries",
		"DROP TABLE IF EXISTS system_metadata",
		"DROP TABLE IF EXISTS goose_db_version",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("reset table with %q: %v", stmt, err)
		}
	}
}
