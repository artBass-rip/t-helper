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

func TestReconcileWorkflowStatusesProcessesBatches(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	for i := 0; i < reconcileWorkflowBatchSize+7; i++ {
		if _, err := store.Enqueue(ctx, EnqueueRequest{
			ID:         newID("job"),
			JobType:    "config_reload",
			JobGroupID: "config_operation:batch_" + newID("group"),
			Payload:    json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if _, err := store.handle.DB.ExecContext(ctx, "DELETE FROM workflow_statuses"); err != nil {
		t.Fatalf("delete workflow statuses: %v", err)
	}
	if err := store.ReconcileWorkflowStatuses(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var count int
	if err := store.handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM workflow_statuses").Scan(&count); err != nil {
		t.Fatalf("count workflow statuses: %v", err)
	}
	if count != reconcileWorkflowBatchSize+7 {
		t.Fatalf("workflow statuses = %d, want %d", count, reconcileWorkflowBatchSize+7)
	}
}

func TestWorkflowStatusSelfHealsMissingReadModel(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	ref, err := store.Enqueue(ctx, EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := store.handle.DB.ExecContext(ctx, "DELETE FROM workflow_statuses"); err != nil {
		t.Fatalf("delete workflow statuses: %v", err)
	}
	status, err := store.WorkflowStatus(ctx, "config_operation:"+ref.JobID)
	if err != nil {
		t.Fatalf("workflow status: %v", err)
	}
	if status.AggregateStatus != StatusQueued || status.ProgressTotal != 1 {
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

func TestGroupedWorkflowIDIsStableAcrossChildJobs(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	groupID := "project_scan:scan_123"
	for _, id := range []string{"job_project_parent", "job_project_child"} {
		if _, err := store.Enqueue(ctx, EnqueueRequest{
			ID:         id,
			JobType:    "project_scan",
			JobGroupID: groupID,
			Payload:    json.RawMessage(`{"schema_version":"jobs.project_scan.payload.v1","project_id":"project_1","scan_type":"full"}`),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	status, err := store.WorkflowStatus(ctx, groupID)
	if err != nil {
		t.Fatalf("workflow status: %v", err)
	}
	if status.WorkflowID != "scan_123" {
		t.Fatalf("workflow id = %q, want scan_123", status.WorkflowID)
	}
	if status.ProgressTotal != 2 {
		t.Fatalf("progress total = %d, want 2", status.ProgressTotal)
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
	completedRef, err := store.Enqueue(ctx, EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue completed: %v", err)
	}
	completed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker-completed", LeaseDuration: time.Hour})
	if err != nil || !ok || completed.ID != completedRef.JobID {
		t.Fatalf("claim completed: ok=%v job=%+v err=%v", ok, completed, err)
	}
	if err := store.Complete(ctx, completed, "host:1:worker-completed", StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), ""); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	if err := store.AddEvent(ctx, Event{JobID: completed.ID, JobGroupID: completed.JobGroupID, EventType: EventProgress, Status: StatusSucceeded, CreatedAt: old}); err != nil {
		t.Fatalf("add old completed event: %v", err)
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
	activeEvents, err := store.ListEvents(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list active events: %v", err)
	}
	foundOldActiveEvent := false
	for _, event := range activeEvents {
		if event.EventType == EventProgress && event.CreatedAt.Equal(old) {
			foundOldActiveEvent = true
		}
	}
	if !foundOldActiveEvent {
		t.Fatalf("old event for active job was deleted: %+v", activeEvents)
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

func TestCleanupRetentionDeletesOldInactiveRowsAcrossBatches(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	ref, err := store.Enqueue(ctx, EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Hour})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	if err := store.Complete(ctx, claimed, "host:1:worker", StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	for i := 0; i < retentionCleanupBatchSize*2+7; i++ {
		if err := store.AddEvent(ctx, Event{
			ID:         newID("event"),
			JobID:      claimed.ID,
			JobGroupID: claimed.JobGroupID,
			EventType:  EventProgress,
			Status:     StatusSucceeded,
			CreatedAt:  old.Add(time.Duration(i) * time.Millisecond),
		}); err != nil {
			t.Fatalf("add old event %d: %v", i, err)
		}
	}
	for i := 0; i < retentionCleanupBatchSize+3; i++ {
		_, err := store.handle.DB.ExecContext(ctx, `INSERT INTO job_locks (id, lock_key, job_id, owner, status, created_at, expires_at, released_at) VALUES (?, ?, ?, ?, 'released', ?, ?, ?)`,
			newID("lock"), "resource:old:"+newID("key"), claimed.ID, "host:1:old", formatTime(old), formatTime(old), formatTime(old))
		if err != nil {
			t.Fatalf("insert old released lock %d: %v", i, err)
		}
	}
	result, err := store.CleanupRetention(ctx, cutoff)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.DeletedJobEvents < retentionCleanupBatchSize*2+7 || result.DeletedJobLocks != retentionCleanupBatchSize+3 {
		t.Fatalf("cleanup did not process all eligible rows across batches: %+v", result)
	}
	events, err := store.ListEvents(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, event := range events {
		if event.EventType == EventProgress && event.CreatedAt.Before(cutoff) {
			t.Fatalf("old inactive event survived cleanup: %+v", event)
		}
	}
	locks, err := store.ListLocks(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	for _, lock := range locks {
		if lock.Status == "released" && lock.ReleasedAt != nil && lock.ReleasedAt.Before(cutoff) {
			t.Fatalf("old released lock survived cleanup: %+v", lock)
		}
	}
}

func TestWorkerStatusesReportEachRunningLease(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	for _, id := range []string{"job_worker_status_1", "job_worker_status_2"} {
		if _, err := store.Enqueue(ctx, EnqueueRequest{
			ID:      id,
			JobType: "config_reload",
			Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Hour}); err != nil || !ok {
			t.Fatalf("claim %d: ok=%v err=%v", i, ok, err)
		}
	}
	workers, err := store.WorkerStatuses(ctx)
	if err != nil {
		t.Fatalf("worker statuses: %v", err)
	}
	if len(workers) != 2 {
		t.Fatalf("worker status rows = %d, want one per running lease: %+v", len(workers), workers)
	}
	seen := map[string]bool{}
	for _, worker := range workers {
		if worker.WorkerID != "host:1:worker" || worker.Status != "active" {
			t.Fatalf("unexpected worker status: %+v", worker)
		}
		seen[worker.RunningJobID] = true
	}
	if !seen["job_worker_status_1"] || !seen["job_worker_status_2"] {
		t.Fatalf("missing running jobs in worker statuses: %+v", workers)
	}
}

func TestWorkerStatusesPageUsesCursor(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	for _, id := range []string{"job_worker_page_1", "job_worker_page_2", "job_worker_page_3"} {
		if _, err := store.Enqueue(ctx, EnqueueRequest{
			ID:      id,
			JobType: "config_reload",
			Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
		claimed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:" + id, LeaseDuration: time.Hour})
		if err != nil || !ok || claimed.ID != id {
			t.Fatalf("claim %s: ok=%v job=%+v err=%v", id, ok, claimed, err)
		}
		time.Sleep(time.Millisecond)
	}
	first, err := store.WorkerStatusesPage(ctx, 2, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := store.WorkerStatusesPage(ctx, 2, first.NextCursor)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	if _, err := store.WorkerStatusesPage(ctx, 2, "not-a-cursor"); err != ErrInvalidCursor {
		t.Fatalf("invalid cursor error = %v, want %v", err, ErrInvalidCursor)
	}
}

func TestRuntimeStatusReportsFailedWhenAllRunningModulesAreFailed(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	if _, err := store.handle.DB.ExecContext(ctx, `INSERT INTO module_states (id, module_name, state, details, updated_at) VALUES (?, ?, 'failed', '{}', ?)`,
		newID("module_state"), "core", formatTime(time.Now().UTC())); err != nil {
		t.Fatalf("mark modules failed: %v", err)
	}
	status, err := store.RuntimeStatus(ctx)
	if err != nil {
		t.Fatalf("runtime status: %v", err)
	}
	if status.Modules["failed"] == 0 {
		t.Fatalf("test did not mark any module failed: %+v", status.Modules)
	}
	if status.AggregateStatus != "failed" {
		t.Fatalf("aggregate status = %q, want failed: %+v", status.AggregateStatus, status)
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

func TestExpiredLeaseRecoveryDoesNotOverrideFreshHeartbeat(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	ref, err := store.Enqueue(ctx, EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Millisecond})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	time.Sleep(5 * time.Millisecond)
	staleSnapshot, err := store.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get stale snapshot: %v", err)
	}
	if staleSnapshot.LeaseExpiresAt == nil || staleSnapshot.LeaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("test did not produce an expired snapshot: %+v", staleSnapshot)
	}
	if err := store.Heartbeat(ctx, claimed.ID, "host:1:worker", time.Hour); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	runtime := NewRuntime(RuntimeOptions{Store: store, WorkerID: "host:1:recovery", LeaseDuration: time.Minute})
	if err := runtime.recoverToQueued(ctx, staleSnapshot, time.Now().UTC()); err != nil {
		t.Fatalf("recover stale snapshot: %v", err)
	}
	current, err := store.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get current: %v", err)
	}
	if current.Status != StatusRunning || current.LeasedBy != "host:1:worker" || current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("fresh heartbeat was overridden by stale recovery: %+v", current)
	}
	events, err := store.ListEvents(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, event := range events {
		if event.EventType == EventLeaseExpired {
			t.Fatalf("stale recovery wrote lease_expired event: %+v", events)
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

func TestResultAndEventPayloadsRedactSecretLikeValues(t *testing.T) {
	ctx := context.Background()
	store := openInternalStore(t)
	ref, err := store.Enqueue(ctx, EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Minute})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	if err := store.AddEvent(ctx, Event{
		JobID:      claimed.ID,
		JobGroupID: claimed.JobGroupID,
		EventType:  EventProgress,
		Status:     StatusRunning,
		Payload:    json.RawMessage(`{"message":"using https://user:pass@example.test/repo.git","details":{"api_token":"abc123"}}`),
	}); err != nil {
		t.Fatalf("add event: %v", err)
	}
	if err := store.Complete(ctx, claimed, "host:1:worker", StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1","token":"secret-token","url":"https://user:pass@example.test/repo.git"}`), ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	done, err := store.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	events, err := store.ListEvents(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	raw := string(done.ResultPayload)
	for _, event := range events {
		raw += string(event.Payload)
	}
	for _, leaked := range []string{"abc123", "secret-token", "user:pass"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("payload leaked %q: %s", leaked, raw)
		}
	}
	if !strings.Contains(raw, "[redacted]") {
		t.Fatalf("payloads did not include redaction marker: %s", raw)
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
