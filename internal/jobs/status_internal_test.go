package jobs

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestReconcileWorkflowStatusesRebuildsMissingReadModel(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	ref, err := store.Enqueue(ctx, EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := store.handle.DB.ExecContext(ctx, "DELETE FROM workflow_statuses"); err != nil {
		t.Fatalf("delete workflow statuses: %v", err)
	}
	if err := store.ReconcileWorkflowStatuses(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status, err := store.WorkflowStatus(ctx, "config_operation:"+ref.JobID)
	if err != nil {
		t.Fatalf("workflow status: %v", err)
	}
	if status.AggregateStatus != StatusQueued || status.ProgressCurrent != 0 || status.ProgressTotal != 1 {
		t.Fatalf("unexpected workflow status: %+v", status)
	}
}

func TestRefreshWorkflowStatusAggregatesMoreThanOneThousandJobs(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	groupID := "config_operation:bulk"
	for i := 0; i < 1005; i++ {
		ref, err := store.Enqueue(ctx, EnqueueRequest{
			JobType:    "config_reload",
			JobGroupID: groupID,
			WorkflowID: "bulk",
			Payload:    json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		})
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		if i < 1000 {
			claimed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Minute})
			if err != nil || !ok || claimed.ID != ref.JobID {
				t.Fatalf("claim %d: ok=%v job=%+v err=%v", i, ok, claimed, err)
			}
			if err := store.Complete(ctx, claimed, "host:1:worker", StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), ""); err != nil {
				t.Fatalf("complete %d: %v", i, err)
			}
		}
	}
	if err := store.RefreshWorkflowStatus(ctx, groupID, "bulk"); err != nil {
		t.Fatalf("refresh workflow: %v", err)
	}
	status, err := store.WorkflowStatus(ctx, groupID)
	if err != nil {
		t.Fatalf("workflow status: %v", err)
	}
	if status.ProgressTotal != 1005 || status.ProgressCurrent != 1000 || status.AggregateStatus != StatusQueued {
		t.Fatalf("unexpected aggregate: %+v", status)
	}
}

func TestCleanupRetentionDeletesOnlyOldInactiveRows(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	ref, err := store.Enqueue(ctx, EnqueueRequest{JobType: "config_reload", LockKey: "resource:one", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Hour})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	if locked, err := store.AcquireLock(ctx, claimed, "host:1:worker", time.Hour); err != nil || !locked {
		t.Fatalf("acquire active lock: locked=%v err=%v", locked, err)
	}
	if err := store.AddEvent(ctx, Event{JobID: claimed.ID, JobGroupID: claimed.JobGroupID, EventType: EventProgress, Status: StatusRunning, CreatedAt: old}); err != nil {
		t.Fatalf("add old event: %v", err)
	}
	_, err = store.handle.DB.ExecContext(ctx, `INSERT INTO job_locks (id, lock_key, job_id, owner, status, created_at, expires_at, released_at) VALUES (?, ?, ?, ?, 'released', ?, ?, ?)`,
		newID("lock"), "resource:old", claimed.ID, "host:1:old", formatTime(old), formatTime(old), formatTime(old))
	if err != nil {
		t.Fatalf("insert old released lock: %v", err)
	}
	result, err := store.CleanupRetention(ctx, cutoff)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.DeletedJobEvents == 0 || result.DeletedJobLocks != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}
	locks, err := store.ListLocks(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	active := 0
	for _, lock := range locks {
		if lock.Status == "held" {
			active++
		}
		if lock.LockKey == "resource:old" {
			t.Fatalf("old released lock was not deleted: %+v", locks)
		}
	}
	if active != 1 {
		t.Fatalf("active lock was not preserved: %+v", locks)
	}
}

func TestWorkerIDAndBackoffDefaults(t *testing.T) {
	workerID := NewWorkerID()
	if !regexp.MustCompile(`^[^:]+:\d+:worker_[0-9a-f]{32}$`).MatchString(workerID) {
		t.Fatalf("worker id %q does not match expected diagnostic format", workerID)
	}
	for attempt, bounds := range map[int][2]time.Duration{
		1: {4 * time.Second, 6 * time.Second},
		2: {8 * time.Second, 12 * time.Second},
		7: {4 * time.Minute, 5 * time.Minute},
	} {
		delay := backoff(attempt)
		if delay < bounds[0] || delay > bounds[1] {
			t.Fatalf("backoff(%d) = %s, want within [%s, %s]", attempt, delay, bounds[0], bounds[1])
		}
	}
}

func TestSafeMessageRedactsSecretLikeValues(t *testing.T) {
	message := safeMessage("failed password=hunter2 token:abc https://user:pass@example.test/repo.git secretref://env/API_TOKEN")
	for _, leaked := range []string{"hunter2", "abc", "user:pass", "API_TOKEN"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("message leaked %q: %s", leaked, message)
		}
	}
	if !strings.Contains(message, "[redacted]") {
		t.Fatalf("expected redaction marker in %q", message)
	}
}

func openInternalStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(ctx, storage.Config{Provider: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := provider.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewStore(handle)
}
