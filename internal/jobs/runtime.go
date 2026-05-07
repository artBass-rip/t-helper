package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
)

type Handler interface {
	Handle(ctx context.Context, job Job) (json.RawMessage, error)
}

type HandlerFunc func(ctx context.Context, job Job) (json.RawMessage, error)

func (f HandlerFunc) Handle(ctx context.Context, job Job) (json.RawMessage, error) {
	return f(ctx, job)
}

type Runtime struct {
	store             *Store
	handlers          map[string]Handler
	workerID          string
	logger            *slog.Logger
	pollInterval      time.Duration
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
}

type RuntimeOptions struct {
	Store             *Store
	Handlers          map[string]Handler
	WorkerID          string
	Logger            *slog.Logger
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
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
	return &Runtime{
		store:             opts.Store,
		handlers:          opts.Handlers,
		workerID:          opts.WorkerID,
		logger:            opts.Logger,
		pollInterval:      opts.PollInterval,
		leaseDuration:     opts.LeaseDuration,
		heartbeatInterval: opts.HeartbeatInterval,
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	if err := r.store.ReconcileWorkflowStatuses(ctx); err != nil {
		r.logger.Warn("reconcile workflow statuses failed", "error", err)
	}
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		if err := r.RecoverExpiredLeases(ctx); err != nil {
			r.logger.Warn("recover expired leases failed", "error", err)
		}
		ran, err := r.RunOnce(ctx)
		if err != nil {
			r.logger.Warn("worker iteration failed", "error", err)
		}
		if ran {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runtime) RunOnce(ctx context.Context) (bool, error) {
	job, ok, err := r.store.ClaimNext(ctx, ClaimOptions{WorkerID: r.workerID, LeaseDuration: r.leaseDuration})
	if err != nil || !ok {
		return ok, err
	}
	if locked, err := r.store.AcquireLock(ctx, job, r.workerID, r.leaseDuration); err != nil {
		return true, err
	} else if !locked {
		runAfter := time.Now().UTC().Add(backoff(job.AttemptCount))
		return true, r.store.Requeue(ctx, job, r.workerID, "lock_contention", runAfter)
	}
	if err := r.store.Start(ctx, job, r.workerID); err != nil {
		return true, err
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
	if err := r.store.AddEvent(ctx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventProgress, Status: StatusRunning, WorkerID: r.workerID, Payload: eventPayload("handler started")}); err != nil {
		return true, err
	}
	if err := r.store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job)); err != nil {
		return true, err
	}

	heartbeatCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go r.heartbeatLoop(heartbeatCtx, job.ID, done)
	result, handleErr := handler.Handle(ctx, job)
	stop()
	<-done

	if handleErr == nil {
		return true, r.store.Complete(ctx, job, r.workerID, StatusSucceeded, result, "")
	}
	classified := classifyError(handleErr)
	if classified.Retryable && job.AttemptCount < job.MaxAttempts {
		return true, r.store.Requeue(ctx, job, r.workerID, classified.Code, time.Now().UTC().Add(backoff(job.AttemptCount)))
	}
	if classified.Code == "cancelled" {
		failure, _ := json.Marshal(FailureResult{
			SchemaVersion: ResultFailureSchemaVersion,
			JobType:       job.JobType,
			WorkerID:      r.workerID,
			Attempt:       job.AttemptCount,
			ErrorCode:     classified.Code,
			Message:       classified.Message,
			Retryable:     false,
		})
		return true, r.store.Complete(ctx, job, r.workerID, StatusCancelled, failure, classified.Message)
	}
	failure, _ := json.Marshal(FailureResult{
		SchemaVersion: ResultFailureSchemaVersion,
		JobType:       job.JobType,
		WorkerID:      r.workerID,
		Attempt:       job.AttemptCount,
		ErrorCode:     classified.Code,
		Message:       classified.Message,
		Retryable:     false,
	})
	return true, r.store.Complete(ctx, job, r.workerID, StatusFailed, failure, classified.Message)
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
			if err := r.store.Heartbeat(context.Background(), jobID, r.workerID, r.leaseDuration); err != nil {
				r.logger.Warn("job heartbeat failed", "job_id", jobID, "error", err)
			}
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
		if err := r.store.AddEvent(ctx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventLeaseExpired, Status: StatusRunning, WorkerID: job.LeasedBy}); err != nil {
			return err
		}
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
	query := `UPDATE jobs SET status = 'queued', leased_by = NULL, lease_expires_at = NULL, heartbeat_at = NULL, run_after = ?, updated_at = ? WHERE id = ? AND status = 'running'`
	args := []any{formatTime(runAfter), formatTime(time.Now().UTC()), job.ID}
	if r.store.handle.Provider == "postgres" {
		query = `UPDATE jobs SET status = 'queued', leased_by = NULL, lease_expires_at = NULL, heartbeat_at = NULL, run_after = $1, updated_at = $2 WHERE id = $3 AND status = 'running'`
	}
	if _, err := r.store.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	if err := r.store.AddEvent(ctx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventRetryScheduled, Status: StatusQueued, WorkerID: r.workerID}); err != nil {
		return err
	}
	return r.store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
}

func (r *Runtime) forceFail(ctx context.Context, job Job, result json.RawMessage, message string) error {
	query := `UPDATE jobs SET status = 'failed', result_payload = ?, error_message = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status = 'running'`
	now := time.Now().UTC()
	args := []any{string(result), message, formatTime(now), formatTime(now), job.ID}
	if r.store.handle.Provider == "postgres" {
		query = `UPDATE jobs SET status = 'failed', result_payload = $1, error_message = $2, finished_at = $3, updated_at = $4 WHERE id = $5 AND status = 'running'`
	}
	if _, err := r.store.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	if err := r.store.AddEvent(ctx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventFailed, Status: StatusFailed, WorkerID: r.workerID}); err != nil {
		return err
	}
	return r.store.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
}

func ModuleHandlers(configStore *appconfig.Store, moduleStore *modules.Store) map[string]Handler {
	return map[string]Handler{
		"config_reload": HandlerFunc(func(ctx context.Context, job Job) (json.RawMessage, error) {
			if err := validatePayloadSchema(job.JobType, job.Payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
			}
			var payload struct {
				Keys []string `json:"keys"`
			}
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
			}
			result, err := configStore.Reload(ctx, payload.Keys)
			if err != nil {
				return nil, HandlerError{Code: "handler_failed", Message: err.Error(), Retryable: true}
			}
			result.SchemaVersion = "jobs.config_reload.result.v1"
			return json.Marshal(result)
		}),
		"module_restart": HandlerFunc(func(ctx context.Context, job Job) (json.RawMessage, error) {
			if err := validatePayloadSchema(job.JobType, job.Payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
			}
			var payload struct {
				ModuleName string `json:"module_name"`
			}
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				return nil, HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
			}
			if payload.ModuleName == "" {
				return nil, HandlerError{Code: "validation_error", Message: "module_name is required", Retryable: false}
			}
			result, err := moduleStore.Restart(ctx, payload.ModuleName, "job:"+job.ID)
			if err != nil {
				return nil, HandlerError{Code: "handler_failed", Message: err.Error(), Retryable: false}
			}
			result.SchemaVersion = "jobs.module_restart.result.v1"
			return json.Marshal(result)
		}),
	}
}

func classifyError(err error) HandlerError {
	var classified HandlerError
	if errors.As(err, &classified) {
		return classified
	}
	if errors.Is(err, context.Canceled) {
		return HandlerError{Code: "cancelled", Message: err.Error(), Retryable: false}
	}
	return HandlerError{Code: "handler_failed", Message: err.Error(), Retryable: true}
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
	return time.Duration(float64(delay) * jitter)
}

func NewWorkerID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), newID("worker"))
}
