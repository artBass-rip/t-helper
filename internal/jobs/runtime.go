package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"time"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
)

type Handler interface {
	Handle(ctx context.Context, env HandlerEnv, job Job) (json.RawMessage, error)
}

type HandlerFunc func(ctx context.Context, env HandlerEnv, job Job) (json.RawMessage, error)

func (f HandlerFunc) Handle(ctx context.Context, env HandlerEnv, job Job) (json.RawMessage, error) {
	return f(ctx, env, job)
}

type HandlerEnv struct {
	WorkerID string
	Logger   *slog.Logger
	Store    *Store
}

func (e HandlerEnv) EmitProgress(ctx context.Context, job Job, message string, details map[string]any) error {
	return e.emit(ctx, job, EventProgress, message, details)
}

func (e HandlerEnv) EmitChildCreated(ctx context.Context, job Job, message string, details map[string]any) error {
	return e.emit(ctx, job, EventChildCreated, message, details)
}

func (e HandlerEnv) emit(ctx context.Context, job Job, eventType, message string, details map[string]any) error {
	if e.Store == nil {
		return fmt.Errorf("handler environment store is nil")
	}
	if err := e.Store.AddEvent(ctx, Event{
		JobID:      job.ID,
		JobGroupID: job.JobGroupID,
		EventType:  eventType,
		Status:     job.Status,
		WorkerID:   e.WorkerID,
		Payload:    eventPayloadDetails(message, details),
	}); err != nil {
		return err
	}
	return e.Store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
}

type Runtime struct {
	store             *Store
	handlers          map[string]Handler
	workerID          string
	logger            *slog.Logger
	pollInterval      time.Duration
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	retentionInterval time.Duration
	retentionTTL      time.Duration
	concurrency       int
}

type RuntimeOptions struct {
	Store             *Store
	Handlers          map[string]Handler
	WorkerID          string
	Logger            *slog.Logger
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	RetentionInterval time.Duration
	RetentionTTL      time.Duration
	Concurrency       int
}

func NewRuntime(opts RuntimeOptions) *Runtime {
	if opts.WorkerID == "" {
		opts.WorkerID = NewWorkerID()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 30 * time.Second
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	if opts.HeartbeatTimeout <= 0 {
		opts.HeartbeatTimeout = 5 * time.Second
	}
	if opts.RetentionInterval <= 0 {
		opts.RetentionInterval = time.Hour
	}
	if opts.RetentionTTL <= 0 {
		opts.RetentionTTL = 30 * 24 * time.Hour
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	return &Runtime{
		store:             opts.Store,
		handlers:          opts.Handlers,
		workerID:          opts.WorkerID,
		logger:            opts.Logger,
		pollInterval:      opts.PollInterval,
		leaseDuration:     opts.LeaseDuration,
		heartbeatInterval: opts.HeartbeatInterval,
		heartbeatTimeout:  opts.HeartbeatTimeout,
		retentionInterval: opts.RetentionInterval,
		retentionTTL:      opts.RetentionTTL,
		concurrency:       opts.Concurrency,
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	if err := r.store.ReconcileWorkflowStatuses(ctx); err != nil {
		r.logger.Warn("reconcile workflow statuses failed", "error", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.maintenanceLoop(ctx)
	}()
	for i := 0; i < r.concurrency; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			r.workerLoop(ctx, slot)
		}(i)
	}
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (r *Runtime) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(r.retentionInterval)
	defer ticker.Stop()
	for {
		if err := r.RecoverExpiredLeases(ctx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("recover expired leases failed", "error", err)
		}
		result, err := r.store.CleanupRetention(ctx, time.Now().UTC().Add(-r.retentionTTL))
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("job retention cleanup failed", "error", err)
		} else if err == nil {
			r.logger.Info("job retention cleanup completed", "deleted_job_events", result.DeletedJobEvents, "deleted_job_locks", result.DeletedJobLocks)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) workerLoop(ctx context.Context, slot int) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		ran, err := r.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("worker iteration failed", "slot", slot, "error", err)
		}
		if ran {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) RunOnce(ctx context.Context) (bool, error) {
	job, ok, err := r.store.ClaimNext(ctx, ClaimOptions{WorkerID: r.workerID, LeaseDuration: r.leaseDuration})
	if err != nil || !ok {
		return ok, err
	}
	handler := r.handlers[job.JobType]
	if handler == nil {
		result, _ := json.Marshal(FailureResult{
			SchemaVersion: ResultFailureSchemaVersion,
			JobType:       job.JobType,
			WorkerID:      r.workerID,
			Attempt:       job.AttemptCount,
			ErrorCode:     "unknown_job_type",
			Message:       "unknown job type",
			Retryable:     false,
		})
		return true, r.store.Complete(ctx, job, r.workerID, StatusFailed, result, "unknown job type")
	}
	if locked, err := r.store.AcquireLock(ctx, job, r.workerID, r.leaseDuration); err != nil {
		return true, r.runtimeRequeue(ctx, job, "transient_error", err)
	} else if !locked {
		return true, r.runtimeRequeue(ctx, job, "lock_contention", nil)
	}
	if err := r.store.Start(ctx, job, r.workerID); err != nil {
		return true, r.runtimeRequeue(ctx, job, "transient_error", err)
	}

	if err := r.store.AddEvent(ctx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventProgress, Status: StatusRunning, WorkerID: r.workerID, Payload: eventPayload("handler started")}); err != nil {
		return true, r.runtimeRequeue(ctx, job, "transient_error", err)
	}
	if err := r.store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job)); err != nil {
		r.logger.Warn("refresh workflow status failed", "job_group_id", job.JobGroupID, "job_id", job.ID, "error", err)
	}

	heartbeatCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go r.heartbeatLoop(heartbeatCtx, job.ID, done)
	env := HandlerEnv{WorkerID: r.workerID, Logger: r.logger, Store: r.store}
	result, handleErr := handler.Handle(ctx, env, job)
	stop()
	<-done
	finalCtx, cancelFinal := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFinal()

	if handleErr == nil {
		return true, r.store.Complete(finalCtx, job, r.workerID, StatusSucceeded, result, "")
	}
	classified := classifyError(handleErr)
	if classified.Code == "cancelled" && ctx.Err() != nil {
		return true, nil
	}
	if classified.Retryable && job.AttemptCount < job.MaxAttempts {
		return true, r.store.Requeue(finalCtx, job, r.workerID, classified.Code, time.Now().UTC().Add(backoff(job.AttemptCount)))
	}
	if classified.Code == "cancelled" {
		failure, _ := json.Marshal(FailureResult{
			SchemaVersion: ResultFailureSchemaVersion,
			JobType:       job.JobType,
			WorkerID:      r.workerID,
			Attempt:       job.AttemptCount,
			ErrorCode:     classified.Code,
			Message:       safeMessage(classified.Message),
			Retryable:     false,
		})
		return true, r.store.Complete(finalCtx, job, r.workerID, StatusCancelled, failure, classified.Message)
	}
	failure, _ := json.Marshal(FailureResult{
		SchemaVersion: ResultFailureSchemaVersion,
		JobType:       job.JobType,
		WorkerID:      r.workerID,
		Attempt:       job.AttemptCount,
		ErrorCode:     classified.Code,
		Message:       safeMessage(classified.Message),
		Retryable:     false,
	})
	return true, r.store.Complete(finalCtx, job, r.workerID, StatusFailed, failure, classified.Message)
}

func (r *Runtime) runtimeRequeue(ctx context.Context, job Job, code string, err error) error {
	message := code
	if err != nil {
		message = code + ": " + safeMessage(err.Error())
	}
	if job.AttemptCount >= job.MaxAttempts {
		failure, _ := json.Marshal(FailureResult{
			SchemaVersion: ResultFailureSchemaVersion,
			JobType:       job.JobType,
			WorkerID:      r.workerID,
			Attempt:       job.AttemptCount,
			ErrorCode:     code,
			Message:       message,
			Retryable:     false,
		})
		return r.store.Complete(ctx, job, r.workerID, StatusFailed, failure, message)
	}
	return r.store.Requeue(ctx, job, r.workerID, code, time.Now().UTC().Add(backoff(job.AttemptCount)))
}

func (r *Runtime) heartbeatLoop(ctx context.Context, jobID string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeatCtx, cancel := context.WithTimeout(context.Background(), r.heartbeatTimeout)
			if err := r.store.Heartbeat(heartbeatCtx, jobID, r.workerID, r.leaseDuration); err != nil {
				r.logger.Warn("job heartbeat failed", "job_id", jobID, "error", err)
			}
			cancel()
		}
	}
}

func (r *Runtime) RecoverExpiredLeases(ctx context.Context) error {
	now := time.Now().UTC()
	query := "SELECT " + r.store.jobSelectColumns() + " FROM jobs WHERE status = 'running' AND lease_expires_at <= ? ORDER BY lease_expires_at ASC LIMIT 50"
	args := []any{formatTime(now)}
	if r.store.handle.Provider == "postgres" {
		query = "SELECT " + r.store.jobSelectColumns() + " FROM jobs WHERE status = 'running' AND lease_expires_at <= $1 ORDER BY lease_expires_at ASC LIMIT 50"
	}
	rows, err := r.store.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var expiredJobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return err
		}
		expiredJobs = append(expiredJobs, job)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, job := range expiredJobs {
		if job.AttemptCount < job.MaxAttempts {
			if err := r.recoverToQueued(ctx, job, now.Add(backoff(job.AttemptCount))); err != nil {
				return err
			}
			continue
		}
		failure, _ := json.Marshal(FailureResult{
			SchemaVersion: ResultFailureSchemaVersion,
			JobType:       job.JobType,
			WorkerID:      r.workerID,
			Attempt:       job.AttemptCount,
			ErrorCode:     "transient_error",
			Message:       "job lease expired and attempts exhausted",
			Retryable:     false,
		})
		if err := r.forceFail(ctx, job, failure, "job lease expired and attempts exhausted"); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) recoverToQueued(ctx context.Context, job Job, runAfter time.Time) error {
	tx, err := r.store.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if job.LeaseExpiresAt == nil {
		return nil
	}
	query := `UPDATE jobs SET status = 'queued', leased_by = NULL, lease_expires_at = NULL, heartbeat_at = NULL, run_after = ?, updated_at = ? WHERE id = ? AND status = 'running' AND leased_by = ? AND lease_expires_at = ?`
	now := time.Now().UTC()
	args := []any{formatTime(runAfter), formatTime(now), job.ID, job.LeasedBy, formatTime(*job.LeaseExpiresAt)}
	if r.store.handle.Provider == "postgres" {
		query = `UPDATE jobs SET status = 'queued', leased_by = NULL, lease_expires_at = NULL, heartbeat_at = NULL, run_after = $1, updated_at = $2 WHERE id = $3 AND status = 'running' AND leased_by = $4 AND lease_expires_at = $5`
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return nil
	}
	if err := r.store.expireJobLocks(ctx, tx, job.ID); err != nil {
		return err
	}
	if err := r.store.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventLeaseExpired, Status: StatusRunning, WorkerID: job.LeasedBy}); err != nil {
		return err
	}
	if err := r.store.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventRetryScheduled, Status: StatusQueued, WorkerID: r.workerID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := r.store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job)); err != nil {
		r.logger.Warn("refresh workflow status failed", "job_group_id", job.JobGroupID, "job_id", job.ID, "error", err)
	}
	return nil
}

func (r *Runtime) forceFail(ctx context.Context, job Job, result json.RawMessage, message string) error {
	tx, err := r.store.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if job.LeaseExpiresAt == nil {
		return nil
	}
	query := `UPDATE jobs SET status = 'failed', result_payload = ?, error_message = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status = 'running' AND leased_by = ? AND lease_expires_at = ?`
	now := time.Now().UTC()
	message = safeMessage(message)
	args := []any{string(result), message, formatTime(now), formatTime(now), job.ID, job.LeasedBy, formatTime(*job.LeaseExpiresAt)}
	if r.store.handle.Provider == "postgres" {
		query = `UPDATE jobs SET status = 'failed', result_payload = $1, error_message = $2, finished_at = $3, updated_at = $4 WHERE id = $5 AND status = 'running' AND leased_by = $6 AND lease_expires_at = $7`
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return nil
	}
	if err := r.store.expireJobLocks(ctx, tx, job.ID); err != nil {
		return err
	}
	if err := r.store.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventLeaseExpired, Status: StatusRunning, WorkerID: job.LeasedBy}); err != nil {
		return err
	}
	if err := r.store.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventFailed, Status: StatusFailed, WorkerID: r.workerID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := r.store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job)); err != nil {
		r.logger.Warn("refresh workflow status failed", "job_group_id", job.JobGroupID, "job_id", job.ID, "error", err)
	}
	return nil
}

func ModuleHandlers(configStore *appconfig.Store, moduleStore *modules.Store) map[string]Handler {
	return map[string]Handler{
		"config_reload": HandlerFunc(func(ctx context.Context, env HandlerEnv, job Job) (json.RawMessage, error) {
			if err := validatePayloadSchema(job.JobType, job.Payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: safeMessage(err.Error()), Retryable: false}
			}
			var payload struct {
				Keys []string `json:"keys"`
			}
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: safeMessage(err.Error()), Retryable: false}
			}
			result, err := configStore.Reload(ctx, payload.Keys)
			if err != nil {
				return nil, HandlerError{Code: "handler_failed", Message: safeMessage(err.Error()), Retryable: true}
			}
			result.SchemaVersion = "jobs.config_reload.result.v1"
			return json.Marshal(result)
		}),
		"module_restart": HandlerFunc(func(ctx context.Context, env HandlerEnv, job Job) (json.RawMessage, error) {
			if err := validatePayloadSchema(job.JobType, job.Payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: safeMessage(err.Error()), Retryable: false}
			}
			var payload struct {
				ModuleName string `json:"module_name"`
			}
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: safeMessage(err.Error()), Retryable: false}
			}
			if payload.ModuleName == "" {
				return nil, HandlerError{Code: "validation_error", Message: "module_name is required", Retryable: false}
			}
			result, err := moduleStore.Restart(ctx, payload.ModuleName, "job:"+job.ID)
			if err != nil {
				return nil, HandlerError{Code: "handler_failed", Message: safeMessage(err.Error()), Retryable: false}
			}
			result.SchemaVersion = "jobs.module_restart.result.v1"
			return json.Marshal(result)
		}),
	}
}

func classifyError(err error) HandlerError {
	var classified HandlerError
	if errors.As(err, &classified) {
		classified.Message = safeMessage(classified.Message)
		return classified
	}
	if errors.Is(err, context.Canceled) {
		return HandlerError{Code: "cancelled", Message: safeMessage(err.Error()), Retryable: false}
	}
	return HandlerError{Code: "handler_failed", Message: safeMessage(err.Error()), Retryable: true}
}

func backoff(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	delay := 5 * time.Second
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= 5*time.Minute {
			delay = 5 * time.Minute
			break
		}
	}
	jitter := 0.8 + rand.Float64()*0.4
	withJitter := time.Duration(float64(delay) * jitter)
	if withJitter > 5*time.Minute {
		return 5 * time.Minute
	}
	return withJitter
}

func NewWorkerID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), newID("worker"))
}
