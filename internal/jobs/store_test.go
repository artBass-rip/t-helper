package jobs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestEnqueueIdempotencyIsScopedByActorAndJobType(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	payload := json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`)
	first, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Actor: "alice", IdempotencyKey: "same", Payload: payload})
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	replay, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Actor: "alice", IdempotencyKey: "same", Payload: payload})
	if err != nil {
		t.Fatalf("enqueue replay: %v", err)
	}
	if replay.JobID != first.JobID {
		t.Fatalf("replay job id = %q, want %q", replay.JobID, first.JobID)
	}
	if _, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Actor: "alice", IdempotencyKey: "same", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.format"]}`)}); err == nil {
		t.Fatal("expected idempotency conflict for different payload")
	}
	otherActor, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Actor: "bob", IdempotencyKey: "same", Payload: payload})
	if err != nil {
		t.Fatalf("enqueue other actor: %v", err)
	}
	if otherActor.JobID == first.JobID {
		t.Fatal("different actor reused job id")
	}
	otherType, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "module_restart", Actor: "alice", IdempotencyKey: "same", Payload: json.RawMessage(`{"schema_version":"jobs.module_restart.payload.v1","module_name":"config-manager"}`)})
	if err != nil {
		t.Fatalf("enqueue other type: %v", err)
	}
	if otherType.JobID == first.JobID {
		t.Fatal("different job type reused job id")
	}
}

func TestClaimHeartbeatCompleteAndStatus(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Second})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok || claimed.ID != ref.JobID || claimed.AttemptCount != 1 || claimed.Status != jobs.StatusRunning {
		t.Fatalf("unexpected claim: ok=%v job=%+v", ok, claimed)
	}
	if err := store.Heartbeat(ctx, claimed.ID, "host:1:worker", 2*time.Second); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	running, err := store.JobStatus(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("job status: %v", err)
	}
	if running.HeartbeatAt == nil || running.LeaseExpiresAt == nil {
		t.Fatalf("missing heartbeat/lease: %+v", running)
	}
	if err := store.Complete(ctx, claimed, "host:1:worker", jobs.StatusSucceeded, json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1","accepted_keys":["logging.level"],"applied_keys":[],"restart_required_keys":[],"failed_keys":[]}`), ""); err != nil {
		t.Fatalf("complete: %v", err)
	}
	done, err := store.JobStatus(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("done status: %v", err)
	}
	if done.Status != jobs.StatusSucceeded || done.LatestEvent == nil {
		t.Fatalf("unexpected done status: %+v", done)
	}
	workflow, err := store.WorkflowStatus(ctx, "config_operation:"+claimed.ID)
	if err != nil {
		t.Fatalf("workflow status: %v", err)
	}
	if workflow.AggregateStatus != jobs.StatusSucceeded || workflow.ProgressCurrent != 1 || workflow.ProgressTotal != 1 {
		t.Fatalf("unexpected workflow: %+v", workflow)
	}
}

func TestLockContentionAndExpiredLeaseRecovery(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	firstRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", LockKey: "resource:one", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	secondRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", LockKey: "resource:one", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	first, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:first", LeaseDuration: time.Minute})
	if err != nil || !ok || first.ID != firstRef.JobID {
		t.Fatalf("claim first: ok=%v job=%+v err=%v", ok, first, err)
	}
	locked, err := store.AcquireLock(ctx, first, "host:1:first", time.Minute)
	if err != nil || !locked {
		t.Fatalf("acquire first lock: locked=%v err=%v", locked, err)
	}
	second, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:second", LeaseDuration: time.Minute})
	if err != nil || !ok || second.ID != secondRef.JobID {
		t.Fatalf("claim second: ok=%v job=%+v err=%v", ok, second, err)
	}
	locked, err = store.AcquireLock(ctx, second, "host:1:second", time.Minute)
	if err != nil {
		t.Fatalf("acquire second lock: %v", err)
	}
	if locked {
		t.Fatal("expected second lock acquisition to be blocked")
	}

	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: store, WorkerID: "host:1:recovery", LeaseDuration: time.Minute})
	expiredRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue expired: %v", err)
	}
	expired, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:expired", LeaseDuration: time.Millisecond})
	if err != nil || !ok || expired.ID != expiredRef.JobID {
		t.Fatalf("claim expired: ok=%v job=%+v err=%v", ok, expired, err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := runtime.RecoverExpiredLeases(ctx); err != nil {
		t.Fatalf("recover expired leases: %v", err)
	}
	recovered, err := store.Get(ctx, expired.ID)
	if err != nil {
		t.Fatalf("get recovered: %v", err)
	}
	if recovered.Status != jobs.StatusQueued || recovered.LeasedBy != "" {
		t.Fatalf("expected recovered queued job, got %+v", recovered)
	}
}

func openStore(t *testing.T) *jobs.Store {
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
	return jobs.NewStore(handle)
}
