package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
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

func TestRuntimeLockContentionDoesNotStartHandler(t *testing.T) {
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
	if locked, err := store.AcquireLock(ctx, first, "host:1:first", time.Minute); err != nil || !locked {
		t.Fatalf("acquire first: locked=%v err=%v", locked, err)
	}
	handled := 0
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:second",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.Job) (json.RawMessage, error) {
				handled++
				return json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), nil
			}),
		},
	})
	ran, err := runtime.RunOnce(ctx)
	if err != nil || !ran {
		t.Fatalf("run once: ran=%v err=%v", ran, err)
	}
	if handled != 0 {
		t.Fatalf("handler ran despite lock contention")
	}
	second, err := store.Get(ctx, secondRef.JobID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if second.Status != jobs.StatusQueued || second.LeasedBy != "" || !second.RunAfter.After(time.Now().UTC()) {
		t.Fatalf("unexpected requeued job: %+v", second)
	}
	events, err := store.ListEvents(ctx, second.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, event := range events {
		if event.EventType == jobs.EventStarted {
			t.Fatalf("lock-contention job wrote started event: %+v", events)
		}
	}
}

func TestHeartbeatExtendsHeldLock(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", LockKey: "resource:one", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Second})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	if locked, err := store.AcquireLock(ctx, claimed, "host:1:worker", time.Second); err != nil || !locked {
		t.Fatalf("acquire lock: locked=%v err=%v", locked, err)
	}
	before, err := store.ListLocks(ctx, claimed.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("locks before: locks=%+v err=%v", before, err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := store.Heartbeat(ctx, claimed.ID, "host:1:worker", time.Minute); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	after, err := store.ListLocks(ctx, claimed.ID)
	if err != nil || len(after) != 1 {
		t.Fatalf("locks after: locks=%+v err=%v", after, err)
	}
	if !after[0].ExpiresAt.After(before[0].ExpiresAt) {
		t.Fatalf("lock expiry was not extended: before=%s after=%s", before[0].ExpiresAt, after[0].ExpiresAt)
	}
}

func TestRuntimeCancellationAndRetryExhaustion(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	cancelledRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue cancelled: %v", err)
	}
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:worker",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.Job) (json.RawMessage, error) {
				return nil, context.Canceled
			}),
		},
	})
	if ran, err := runtime.RunOnce(ctx); err != nil || !ran {
		t.Fatalf("run cancelled: ran=%v err=%v", ran, err)
	}
	cancelled, err := store.Get(ctx, cancelledRef.JobID)
	if err != nil {
		t.Fatalf("get cancelled: %v", err)
	}
	if cancelled.Status != jobs.StatusCancelled {
		t.Fatalf("cancelled status = %q", cancelled.Status)
	}

	failedRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", MaxAttempts: 1, Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	runtime = jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:worker",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.Job) (json.RawMessage, error) {
				return nil, errors.New("temporary problem")
			}),
		},
	})
	if ran, err := runtime.RunOnce(ctx); err != nil || !ran {
		t.Fatalf("run failed: ran=%v err=%v", ran, err)
	}
	failed, err := store.Get(ctx, failedRef.JobID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if failed.Status != jobs.StatusFailed {
		t.Fatalf("failed status = %q", failed.Status)
	}
	var failure jobs.FailureResult
	if err := json.Unmarshal(failed.ResultPayload, &failure); err != nil {
		t.Fatalf("decode failure result: %v", err)
	}
	if failure.SchemaVersion != jobs.ResultFailureSchemaVersion || failure.ErrorCode != "handler_failed" || failure.Attempt != 1 {
		t.Fatalf("unexpected failure result: %+v", failure)
	}
}

func TestEnqueueRejectsWrongPayloadSchemaVersion(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	_, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"wrong.v1"}`)})
	if err == nil {
		t.Fatal("expected schema validation error")
	}
}

func TestConcurrentWorkersClaimOnlyOneQueuedJob(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
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
