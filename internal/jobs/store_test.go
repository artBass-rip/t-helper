package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	replayWithDifferentJSONFormatting, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Actor: "alice", IdempotencyKey: "same", Payload: json.RawMessage(`{
		"keys": ["logging.level"],
		"schema_version": "jobs.config_reload.payload.v1"
	}`)})
	if err != nil {
		t.Fatalf("enqueue replay with different JSON formatting: %v", err)
	}
	if replayWithDifferentJSONFormatting.JobID != first.JobID {
		t.Fatalf("formatted replay job id = %q, want %q", replayWithDifferentJSONFormatting.JobID, first.JobID)
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

func TestConcurrentEnqueueWithSameIdempotencyKeyReplaysJobRef(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	payload := json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`)
	const workers = 16
	refs := make(chan jobs.JobRef, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
				JobType:        "config_reload",
				Actor:          "alice",
				IdempotencyKey: "concurrent",
				Payload:        payload,
			})
			if err != nil {
				errs <- err
				return
			}
			refs <- ref
		}()
	}
	wg.Wait()
	close(refs)
	close(errs)
	for err := range errs {
		t.Fatalf("enqueue failed: %v", err)
	}
	var first string
	count := 0
	for ref := range refs {
		if first == "" {
			first = ref.JobID
		}
		if ref.JobID != first {
			t.Fatalf("got job id %q, want replay of %q", ref.JobID, first)
		}
		count++
	}
	if count != workers {
		t.Fatalf("refs = %d, want %d", count, workers)
	}
	items, err := store.List(ctx, jobs.ListFilters{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("jobs created = %d, want 1: %+v", len(items), items)
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

func TestExpiredLeaseRecoveryExpiresHeldLocks(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		LockKey: "resource:expired",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:expired", LeaseDuration: time.Millisecond})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	if locked, err := store.AcquireLock(ctx, claimed, "host:1:expired", time.Hour); err != nil || !locked {
		t.Fatalf("acquire lock: locked=%v err=%v", locked, err)
	}
	time.Sleep(5 * time.Millisecond)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: store, WorkerID: "host:1:recovery", LeaseDuration: time.Minute})
	if err := runtime.RecoverExpiredLeases(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	locks, err := store.ListLocks(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	if len(locks) != 1 || locks[0].Status != "expired" {
		t.Fatalf("expected held lock to expire, got %+v", locks)
	}
	nextRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		LockKey: "resource:expired",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue next: %v", err)
	}
	next, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:next", LeaseDuration: time.Minute})
	if err != nil || !ok || next.ID != nextRef.JobID {
		t.Fatalf("claim next: ok=%v job=%+v err=%v", ok, next, err)
	}
	if locked, err := store.AcquireLock(ctx, next, "host:1:next", time.Minute); err != nil || !locked {
		t.Fatalf("new lock should not be blocked by expired lock: locked=%v err=%v", locked, err)
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
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
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
		if event.EventType == jobs.EventRetryScheduled {
			var payload struct {
				Details map[string]any `json:"details"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode retry payload: %v", err)
			}
			if payload.Details["error_code"] != "lock_contention" || payload.Details["lock_key"] != "resource:one" {
				t.Fatalf("retry payload missing machine-readable lock contention details: %+v", payload.Details)
			}
		}
	}
}

func TestRuntimeLockContentionExhaustsAttempts(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	firstRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		LockKey: "resource:exhausted",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	secondRef, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:     "config_reload",
		LockKey:     "resource:exhausted",
		MaxAttempts: 1,
		Payload:     json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
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

	handled := false
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:second",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
				handled = true
				return json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), nil
			}),
		},
	})
	ran, err := runtime.RunOnce(ctx)
	if err != nil || !ran {
		t.Fatalf("run once: ran=%v err=%v", ran, err)
	}
	if handled {
		t.Fatal("handler ran despite lock contention")
	}
	second, err := store.Get(ctx, secondRef.JobID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if second.Status != jobs.StatusFailed || second.AttemptCount != 1 {
		t.Fatalf("expected failed exhausted lock contention job, got %+v", second)
	}
	var failure jobs.FailureResult
	if err := json.Unmarshal(second.ResultPayload, &failure); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	if failure.ErrorCode != "lock_contention" || failure.Retryable {
		t.Fatalf("unexpected failure result: %+v", failure)
	}
}

func TestClaimNextSkipsQueuedJobsWithExhaustedAttempts(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:     "config_reload",
		MaxAttempts: 1,
		Payload:     json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:first", LeaseDuration: time.Minute})
	if err != nil || !ok || claimed.ID != ref.JobID {
		t.Fatalf("claim first: ok=%v job=%+v err=%v", ok, claimed, err)
	}
	if err := store.Requeue(ctx, claimed, "host:1:first", "transient_error", time.Now().UTC()); err != nil {
		t.Fatalf("requeue exhausted job: %v", err)
	}
	claimed, ok, err = store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:second", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("claim exhausted: %v", err)
	}
	if ok {
		t.Fatalf("claimed exhausted queued job: %+v", claimed)
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
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
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
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
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

func TestRuntimeFinalizesCancelledHandlerAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := openStore(t)
	ref, err := store.Enqueue(context.Background(), jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:worker",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
				cancel()
				return nil, context.Canceled
			}),
		},
	})
	if ran, err := runtime.RunOnce(ctx); err != nil || !ran {
		t.Fatalf("run cancelled: ran=%v err=%v", ran, err)
	}
	cancelled, err := store.Get(context.Background(), ref.JobID)
	if err != nil {
		t.Fatalf("get cancelled: %v", err)
	}
	if cancelled.Status != jobs.StatusCancelled || cancelled.FinishedAt == nil {
		t.Fatalf("job was not finalized after context cancellation: %+v", cancelled)
	}
}

func TestRuntimeFailurePayloadRedactsSecretLikeValues(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:     "config_reload",
		MaxAttempts: 1,
		Payload:     json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:worker",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
				return nil, errors.New("failed password=hunter2 token:abc https://user:pass@example.test/repo.git")
			}),
		},
	})
	if ran, err := runtime.RunOnce(ctx); err != nil || !ran {
		t.Fatalf("run once: ran=%v err=%v", ran, err)
	}
	failed, err := store.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	raw := string(failed.ResultPayload) + failed.ErrorMessage
	for _, leaked := range []string{"hunter2", "abc", "user:pass"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("failure payload leaked %q: %s", leaked, raw)
		}
	}
}

func TestRuntimePassesHandlerEnvironment(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:worker",
		Handlers: map[string]jobs.Handler{
			"config_reload": jobs.HandlerFunc(func(ctx context.Context, env jobs.HandlerEnv, job jobs.Job) (json.RawMessage, error) {
				if env.WorkerID != "host:1:worker" || env.Store == nil || env.Logger == nil {
					t.Fatalf("handler environment was not populated: %+v", env)
				}
				if err := env.EmitProgress(ctx, job, "handler progress", map[string]any{"phase": "test"}); err != nil {
					return nil, err
				}
				return json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), nil
			}),
		},
	})
	ran, err := runtime.RunOnce(ctx)
	if err != nil || !ran {
		t.Fatalf("run once: ran=%v err=%v", ran, err)
	}
	events, err := store.ListEvents(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, event := range events {
		if event.EventType == jobs.EventProgress && strings.Contains(string(event.Payload), "handler progress") {
			found = true
		}
	}
	if !found {
		t.Fatalf("handler progress event not found: %+v", events)
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

func TestClaimOrderingUsesRunAfterPriorityCreatedAt(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	now := time.Now().UTC()
	lowPriority, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		ID:       "job_low_priority",
		JobType:  "config_reload",
		RunAfter: now,
		Priority: 1,
		Payload:  json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue low priority: %v", err)
	}
	highPriority, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		ID:       "job_high_priority",
		JobType:  "config_reload",
		RunAfter: now,
		Priority: 10,
		Payload:  json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue high priority: %v", err)
	}
	future, err := store.Enqueue(ctx, jobs.EnqueueRequest{
		ID:       "job_future",
		JobType:  "config_reload",
		RunAfter: now.Add(time.Hour),
		Priority: 100,
		Payload:  json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
	})
	if err != nil {
		t.Fatalf("enqueue future: %v", err)
	}

	claimed, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:worker", Now: now, LeaseDuration: time.Minute})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if claimed.ID != highPriority.JobID {
		t.Fatalf("claimed %q, want high priority %q; low=%q future=%q", claimed.ID, highPriority.JobID, lowPriority.JobID, future.JobID)
	}
}

func TestJobsListCursorPagination(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	for _, id := range []string{"job_page_1", "job_page_2", "job_page_3"} {
		if _, err := store.Enqueue(ctx, jobs.EnqueueRequest{
			ID:      id,
			JobType: "config_reload",
			Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
		time.Sleep(time.Millisecond)
	}
	first, err := store.ListPage(ctx, jobs.ListFilters{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := store.ListPage(ctx, jobs.ListFilters{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	if first.Items[0].ID == second.Items[0].ID || first.Items[1].ID == second.Items[0].ID {
		t.Fatalf("cursor repeated item: first=%+v second=%+v", first.Items, second.Items)
	}
}

func TestConcurrentRuntimeWorkersRunNonConflictingLocks(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	for _, lockKey := range []string{"resource:one", "resource:two"} {
		if _, err := store.Enqueue(ctx, jobs.EnqueueRequest{
			JobType: "config_reload",
			LockKey: lockKey,
			Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", lockKey, err)
		}
	}

	var handled atomic.Int32
	handler := jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
		handled.Add(1)
		time.Sleep(10 * time.Millisecond)
		return json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), nil
	})
	var wg sync.WaitGroup
	for _, workerID := range []string{"host:1:first", "host:1:second"} {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			runtime := jobs.NewRuntime(jobs.RuntimeOptions{
				Store:    store,
				WorkerID: workerID,
				Handlers: map[string]jobs.Handler{
					"config_reload": handler,
				},
				HeartbeatInterval: time.Millisecond,
				LeaseDuration:     time.Second,
			})
			ran, err := runtime.RunOnce(ctx)
			if err != nil {
				t.Errorf("run once %s: %v", workerID, err)
			}
			if !ran {
				t.Errorf("worker %s did not claim a job", workerID)
			}
		}(workerID)
	}
	wg.Wait()
	if handled.Load() != 2 {
		t.Fatalf("handled jobs = %d, want 2", handled.Load())
	}
	items, err := store.List(ctx, jobs.ListFilters{Status: jobs.StatusSucceeded})
	if err != nil {
		t.Fatalf("list succeeded: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("succeeded jobs = %d, want 2: %+v", len(items), items)
	}
}

func TestRuntimeRunHonorsInProcessConcurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := openStore(t)
	for _, lockKey := range []string{"resource:one", "resource:two"} {
		if _, err := store.Enqueue(context.Background(), jobs.EnqueueRequest{
			JobType: "config_reload",
			LockKey: lockKey,
			Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", lockKey, err)
		}
	}

	var active atomic.Int32
	var maxActive atomic.Int32
	done := make(chan struct{}, 2)
	handler := jobs.HandlerFunc(func(context.Context, jobs.HandlerEnv, jobs.Job) (json.RawMessage, error) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		active.Add(-1)
		done <- struct{}{}
		return json.RawMessage(`{"schema_version":"jobs.config_reload.result.v1"}`), nil
	})
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    store,
		WorkerID: "host:1:worker",
		Handlers: map[string]jobs.Handler{
			"config_reload": handler,
		},
		PollInterval:      time.Millisecond,
		HeartbeatInterval: time.Millisecond,
		LeaseDuration:     time.Second,
		Concurrency:       2,
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Run(ctx)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("runtime did not process both jobs")
		}
	}
	if maxActive.Load() < 2 {
		t.Fatalf("max active handlers = %d, want at least 2", maxActive.Load())
	}
	deadline := time.After(time.Second)
	for {
		items, err := store.List(context.Background(), jobs.ListFilters{Status: jobs.StatusSucceeded})
		if err != nil {
			t.Fatalf("list succeeded: %v", err)
		}
		if len(items) == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("succeeded jobs = %d, want 2: %+v", len(items), items)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after cancellation")
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
