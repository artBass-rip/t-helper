package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/storage"
)

var (
	ErrNotFound            = errors.New("job not found")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrInvalidCursor       = errors.New("invalid cursor")
)

type Store struct {
	handle *storage.Handle
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func NewStore(handle *storage.Handle) *Store {
	return &Store{handle: handle}
}

func NewJobID() string {
	return newID("job")
}

func (s *Store) Enqueue(ctx context.Context, req EnqueueRequest) (JobRef, error) {
	now := time.Now().UTC()
	if req.ID == "" {
		req.ID = NewJobID()
	}
	if strings.TrimSpace(req.Actor) == "" {
		req.Actor = "system"
	}
	if req.MaxAttempts == 0 {
		req.MaxAttempts = 3
	}
	if req.RunAfter.IsZero() {
		req.RunAfter = now
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{}`)
	}
	if err := validatePayloadSchema(req.JobType, req.Payload); err != nil {
		return JobRef{}, err
	}
	if err := validatePayloadContract(req.JobType, req.Payload); err != nil {
		return JobRef{}, err
	}
	if err := validateSafeJobPayload(req.Payload); err != nil {
		return JobRef{}, err
	}
	if req.JobGroupID == "" {
		req.JobGroupID = defaultJobGroupID(req.JobType, req.ID)
	}
	if req.WorkflowID == "" {
		req.WorkflowID = workflowIDFromGroupID(req.JobGroupID, req.ID)
	}

	if req.IdempotencyKey != "" {
		if existing, err := s.findByIdempotency(ctx, req.Actor, req.JobType, req.IdempotencyKey); err == nil {
			samePayload, err := sameJSON(existing.Payload, req.Payload)
			if err != nil {
				return JobRef{}, err
			}
			if !samePayload {
				return JobRef{}, fmt.Errorf("%w for %s/%s", ErrIdempotencyConflict, req.JobType, req.IdempotencyKey)
			}
			return JobRef{JobID: existing.ID, Status: existing.Status, SchemaVersion: JobRefSchemaVersion}, nil
		} else if !errors.Is(err, ErrNotFound) {
			return JobRef{}, err
		}
	}

	query := `INSERT INTO jobs (id, job_type, status, actor, correlation_id, idempotency_key, parent_job_id, job_group_id, lock_key, attempt_count, max_attempts, run_after, priority, payload, created_at, updated_at)
VALUES (?, ?, 'queued', ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`
	args := []any{req.ID, req.JobType, nullEmpty(req.Actor), nullEmpty(req.CorrelationID), nullEmpty(req.IdempotencyKey), nullEmpty(req.ParentJobID), req.JobGroupID, nullEmpty(req.LockKey), req.MaxAttempts, formatTime(req.RunAfter), req.Priority, string(req.Payload), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO jobs (id, job_type, status, actor, correlation_id, idempotency_key, parent_job_id, job_group_id, lock_key, attempt_count, max_attempts, run_after, priority, payload, created_at, updated_at)
VALUES ($1, $2, 'queued', $3, $4, $5, $6, $7, $8, 0, $9, $10, $11, $12, $13, $14)`
	}
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return JobRef{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		if req.IdempotencyKey != "" && isUniqueConstraintError(err) {
			_ = tx.Rollback()
			return s.idempotentReplay(ctx, req)
		}
		return JobRef{}, err
	}
	if err := s.addEvent(ctx, tx, Event{JobID: req.ID, JobGroupID: req.JobGroupID, EventType: EventQueued, Status: StatusQueued}); err != nil {
		return JobRef{}, err
	}
	if err := tx.Commit(); err != nil {
		return JobRef{}, err
	}
	_ = s.RefreshWorkflowStatus(ctx, req.JobGroupID, req.WorkflowID)
	return JobRef{JobID: req.ID, Status: StatusQueued, SchemaVersion: JobRefSchemaVersion}, nil
}

func (s *Store) EnqueueIfNoActive(ctx context.Context, req EnqueueRequest) (JobRef, bool, error) {
	if strings.TrimSpace(req.LockKey) == "" {
		ref, err := s.Enqueue(ctx, req)
		return ref, true, err
	}
	existing, err := s.findActiveByLock(ctx, req.JobType, req.LockKey)
	if err == nil {
		return JobRef{JobID: existing.ID, Status: existing.Status, SchemaVersion: JobRefSchemaVersion}, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return JobRef{}, false, err
	}
	ref, err := s.Enqueue(ctx, req)
	if err != nil {
		return JobRef{}, false, err
	}
	return ref, true, nil
}

func (s *Store) ActiveRepositoryOperation(ctx context.Context, lockKey string) (Job, error) {
	return s.findActiveRepositoryOperation(ctx, lockKey)
}

func (s *Store) ActiveRepositoryOperationByID(ctx context.Context, id string) (Job, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if job.Status != StatusQueued && job.Status != StatusRunning {
		return Job{}, ErrNotFound
	}
	switch job.JobType {
	case "repo_clone", "repo_pull", "repo_sync":
		return job, nil
	default:
		return Job{}, ErrNotFound
	}
}

func (s *Store) IdempotentReplay(ctx context.Context, req EnqueueRequest) (JobRef, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return JobRef{}, ErrNotFound
	}
	if strings.TrimSpace(req.Actor) == "" {
		req.Actor = "system"
	}
	return s.idempotentReplay(ctx, req)
}

func (s *Store) JobByIdempotency(ctx context.Context, actor, jobType, key string) (Job, error) {
	if strings.TrimSpace(key) == "" {
		return Job{}, ErrNotFound
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	return s.findByIdempotency(ctx, actor, jobType, key)
}

func (s *Store) idempotentReplay(ctx context.Context, req EnqueueRequest) (JobRef, error) {
	existing, err := s.findByIdempotency(ctx, req.Actor, req.JobType, req.IdempotencyKey)
	if err != nil {
		return JobRef{}, err
	}
	samePayload, err := sameJSON(existing.Payload, req.Payload)
	if err != nil {
		return JobRef{}, err
	}
	if !samePayload {
		return JobRef{}, fmt.Errorf("%w for %s/%s", ErrIdempotencyConflict, req.JobType, req.IdempotencyKey)
	}
	return JobRef{JobID: existing.ID, Status: existing.Status, SchemaVersion: JobRefSchemaVersion}, nil
}

func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE id = $1"
	}
	row := s.handle.DB.QueryRowContext(ctx, query, args...)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

func (s *Store) List(ctx context.Context, filters ListFilters) ([]Job, error) {
	page, err := s.list(ctx, filters, 200)
	return page.Items, err
}

func (s *Store) ListPage(ctx context.Context, filters ListFilters) (Page[Job], error) {
	return s.list(ctx, filters, 200)
}

func (s *Store) list(ctx context.Context, filters ListFilters, maxLimit int) (Page[Job], error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if filters.JobType != "" {
		add("job_type = %s", filters.JobType)
	}
	if filters.Status != "" {
		add("status = %s", filters.Status)
	}
	if filters.LockKey != "" {
		add("lock_key = %s", filters.LockKey)
	}
	if filters.JobGroupID != "" {
		add("job_group_id = %s", filters.JobGroupID)
	}
	if filters.ParentJobID != "" {
		add("parent_job_id = %s", filters.ParentJobID)
	}
	if filters.Cursor != "" {
		cursor, err := decodeCursor(filters.Cursor)
		if err != nil {
			return Page[Job]{}, err
		}
		args = append(args, formatTime(cursor.Time), formatTime(cursor.Time), cursor.ID)
		where = append(where, fmt.Sprintf("(created_at < %s OR (created_at = %s AND id < %s))", s.placeholder(len(args)-2), s.placeholder(len(args)-1), s.placeholder(len(args))))
	}
	limit := filters.Limit
	if maxLimit <= 0 {
		maxLimit = 200
	}
	if limit <= 0 || limit > maxLimit {
		limit = 100
		if maxLimit > 200 {
			limit = maxLimit
		}
	}
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit+1)
	query += " ORDER BY created_at DESC, id DESC LIMIT " + s.placeholder(len(args))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[Job]{}, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return Page[Job]{}, err
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return Page[Job]{}, err
	}
	var next string
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return Page[Job]{Items: out, NextCursor: next}, nil
}

type ListFilters struct {
	JobType     string
	Status      string
	LockKey     string
	JobGroupID  string
	ParentJobID string
	Limit       int
	Cursor      string
}

func (s *Store) ClaimNext(ctx context.Context, opts ClaimOptions) (Job, bool, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 30 * time.Second
	}
	leaseExpires := opts.Now.Add(opts.LeaseDuration)
	if s.handle.Provider == "postgres" {
		tx, err := s.handle.DB.BeginTx(ctx, nil)
		if err != nil {
			return Job{}, false, err
		}
		defer tx.Rollback()
		query := `UPDATE jobs SET status = 'running', leased_by = $1, lease_expires_at = $2, heartbeat_at = $3, attempt_count = attempt_count + 1, updated_at = $3
WHERE id = (
  SELECT id FROM jobs
	  WHERE status = 'queued' AND run_after <= $3 AND attempt_count < max_attempts
  ORDER BY run_after ASC, priority DESC, created_at ASC
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
RETURNING ` + s.jobSelectColumns()
		job, err := scanJob(tx.QueryRowContext(ctx, query, opts.WorkerID, formatTime(leaseExpires), formatTime(opts.Now)))
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, false, nil
		}
		if err != nil {
			return Job{}, false, err
		}
		if err := s.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventClaimed, Status: StatusRunning, WorkerID: opts.WorkerID}); err != nil {
			return Job{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Job{}, false, err
		}
		_ = s.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
		return job, true, nil
	}

	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, false, err
	}
	defer tx.Rollback()
	var candidateID string
	selectCandidate := `SELECT id FROM jobs
WHERE status = 'queued' AND run_after <= ? AND attempt_count < max_attempts
ORDER BY run_after ASC, priority DESC, created_at ASC
LIMIT 1`
	err = tx.QueryRowContext(ctx, selectCandidate, formatTime(opts.Now)).Scan(&candidateID)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	update := `UPDATE jobs SET status = 'running', leased_by = ?, lease_expires_at = ?, heartbeat_at = ?, attempt_count = attempt_count + 1, updated_at = ?
WHERE id = ? AND status = 'queued'`
	res, err := tx.ExecContext(ctx, update, opts.WorkerID, formatTime(leaseExpires), formatTime(opts.Now), formatTime(opts.Now), candidateID)
	if err != nil {
		return Job{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Job{}, false, err
	}
	if affected == 0 {
		return Job{}, false, nil
	}
	job, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+s.jobSelectColumns()+" FROM jobs WHERE id = ?", candidateID))
	if err != nil {
		return Job{}, false, err
	}
	if err := s.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventClaimed, Status: StatusRunning, WorkerID: opts.WorkerID}); err != nil {
		return Job{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, err
	}
	_ = s.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
	return job, true, nil
}

func (s *Store) Start(ctx context.Context, job Job, workerID string) error {
	now := time.Now().UTC()
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE jobs SET started_at = COALESCE(started_at, ?), updated_at = ? WHERE id = ? AND leased_by = ? AND status = 'running'`
	args := []any{formatTime(now), formatTime(now), job.ID, workerID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE jobs SET started_at = COALESCE(started_at, $1), updated_at = $2 WHERE id = $3 AND leased_by = $4 AND status = 'running'`
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return fmt.Errorf("job %s is not running under worker lease %s", job.ID, workerID)
	}
	if err := s.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventStarted, Status: StatusRunning, WorkerID: workerID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
	return nil
}

func (s *Store) Heartbeat(ctx context.Context, jobID, workerID string, leaseDuration time.Duration) error {
	now := time.Now().UTC()
	query := `UPDATE jobs SET heartbeat_at = ?, lease_expires_at = ?, updated_at = ? WHERE id = ? AND leased_by = ? AND status = 'running'`
	args := []any{formatTime(now), formatTime(now.Add(leaseDuration)), formatTime(now), jobID, workerID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE jobs SET heartbeat_at = $1, lease_expires_at = $2, updated_at = $3 WHERE id = $4 AND leased_by = $5 AND status = 'running'`
	}
	res, err := s.handle.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil
	}
	return s.ExtendLocks(ctx, jobID, workerID, now.Add(leaseDuration))
}

func (s *Store) AcquireLock(ctx context.Context, job Job, workerID string, leaseDuration time.Duration) (bool, error) {
	if job.LockKey == "" {
		return true, nil
	}
	now := time.Now().UTC()
	if err := s.ExpireLocks(ctx, now); err != nil {
		return false, err
	}
	query := `INSERT INTO job_locks (id, lock_key, job_id, owner, status, created_at, expires_at) VALUES (?, ?, ?, ?, 'held', ?, ?)`
	args := []any{newID("lock"), job.LockKey, job.ID, workerID, formatTime(now), formatTime(now.Add(leaseDuration))}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO job_locks (id, lock_key, job_id, owner, status, created_at, expires_at) VALUES ($1, $2, $3, $4, 'held', $5, $6)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Store) ExtendLocks(ctx context.Context, jobID, workerID string, expiresAt time.Time) error {
	query := `UPDATE job_locks SET expires_at = ? WHERE job_id = ? AND owner = ? AND status = 'held'`
	args := []any{formatTime(expiresAt), jobID, workerID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE job_locks SET expires_at = $1 WHERE job_id = $2 AND owner = $3 AND status = 'held'`
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ReleaseLocks(ctx context.Context, jobID, workerID string) error {
	return s.releaseLocks(ctx, s.handle.DB, jobID, workerID, time.Now().UTC())
}

func (s *Store) ExpireLocks(ctx context.Context, now time.Time) error {
	query := `UPDATE job_locks SET status = 'expired' WHERE status = 'held' AND expires_at <= ?`
	args := []any{formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `UPDATE job_locks SET status = 'expired' WHERE status = 'held' AND expires_at <= $1`
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) expireJobLocks(ctx context.Context, exec sqlExecutor, jobID string) error {
	query := `UPDATE job_locks SET status = 'expired' WHERE job_id = ? AND status = 'held'`
	args := []any{jobID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE job_locks SET status = 'expired' WHERE job_id = $1 AND status = 'held'`
	}
	_, err := exec.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) Complete(ctx context.Context, job Job, workerID, status string, result json.RawMessage, message string) error {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	result = safeJSONPayload(result)
	message = safeMessage(message)
	now := time.Now().UTC()
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE jobs SET status = ?, result_payload = ?, error_message = ?, finished_at = ?, updated_at = ? WHERE id = ? AND leased_by = ? AND status = 'running'`
	args := []any{status, string(result), nullEmpty(message), formatTime(now), formatTime(now), job.ID, workerID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE jobs SET status = $1, result_payload = $2, error_message = $3, finished_at = $4, updated_at = $5 WHERE id = $6 AND leased_by = $7 AND status = 'running'`
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return fmt.Errorf("job %s is not running under worker lease %s", job.ID, workerID)
	}
	if err := s.releaseLocks(ctx, tx, job.ID, workerID, now); err != nil {
		return err
	}
	eventType := EventSucceeded
	if status == StatusFailed {
		eventType = EventFailed
	} else if status == StatusCancelled {
		eventType = EventCancelled
	}
	if err := s.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: eventType, Status: status, WorkerID: workerID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
	return nil
}

func (s *Store) Requeue(ctx context.Context, job Job, workerID, reason string, runAfter time.Time) error {
	now := time.Now().UTC()
	reason = safeMessage(reason)
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE jobs SET status = 'queued', leased_by = NULL, lease_expires_at = NULL, heartbeat_at = NULL, run_after = ?, error_message = ?, updated_at = ? WHERE id = ? AND leased_by = ? AND status = 'running'`
	args := []any{formatTime(runAfter), nullEmpty(reason), formatTime(now), job.ID, workerID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE jobs SET status = 'queued', leased_by = NULL, lease_expires_at = NULL, heartbeat_at = NULL, run_after = $1, error_message = $2, updated_at = $3 WHERE id = $4 AND leased_by = $5 AND status = 'running'`
	}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return fmt.Errorf("job %s is not running under worker lease %s", job.ID, workerID)
	}
	if err := s.releaseLocks(ctx, tx, job.ID, workerID, now); err != nil {
		return err
	}
	if err := s.addEvent(ctx, tx, Event{JobID: job.ID, JobGroupID: job.JobGroupID, EventType: EventRetryScheduled, Status: StatusQueued, WorkerID: workerID, Payload: eventPayloadDetails(reason, map[string]any{"error_code": reason, "lock_key": job.LockKey, "retry_after": runAfter})}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.RefreshWorkflowStatus(ctx, job.JobGroupID, workflowIDForJob(job))
	return nil
}

func (s *Store) releaseLocks(ctx context.Context, exec sqlExecutor, jobID, workerID string, releasedAt time.Time) error {
	query := `UPDATE job_locks SET status = 'released', released_at = ? WHERE job_id = ? AND owner = ? AND status = 'held'`
	args := []any{formatTime(releasedAt), jobID, workerID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE job_locks SET status = 'released', released_at = $1 WHERE job_id = $2 AND owner = $3 AND status = 'held'`
	}
	_, err := exec.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) AddEvent(ctx context.Context, event Event) error {
	return s.addEvent(ctx, s.handle.DB, event)
}

func (s *Store) addEvent(ctx context.Context, exec sqlExecutor, event Event) error {
	if event.ID == "" {
		event.ID = newID("event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.Payload = safeJSONPayload(event.Payload)
	query := `INSERT INTO job_events (id, job_id, job_group_id, event_type, status, worker_id, metric_name, metric_value, payload, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{event.ID, event.JobID, nullEmpty(event.JobGroupID), event.EventType, nullEmpty(event.Status), nullEmpty(event.WorkerID), nullEmpty(event.MetricName), event.MetricValue, nullJSON(event.Payload), formatTime(event.CreatedAt)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO job_events (id, job_id, job_group_id, event_type, status, worker_id, metric_name, metric_value, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	}
	_, err := exec.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) LatestEvent(ctx context.Context, jobID string) (*Event, error) {
	query := `SELECT id, job_id, COALESCE(job_group_id, ''), event_type, COALESCE(status, ''), COALESCE(worker_id, ''), COALESCE(metric_name, ''), metric_value, COALESCE(payload, ''), ` + s.timeExpr("created_at") + ` FROM job_events WHERE job_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`
	args := []any{jobID}
	if s.handle.Provider == "postgres" {
		query = `SELECT id, job_id, COALESCE(job_group_id, ''), event_type, COALESCE(status, ''), COALESCE(worker_id, ''), COALESCE(metric_name, ''), metric_value, COALESCE(payload::text, ''), ` + s.timeExpr("created_at") + ` FROM job_events WHERE job_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`
	}
	ev, err := scanEvent(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ev, err
}

func (s *Store) ListEvents(ctx context.Context, jobID string) ([]Event, error) {
	query := `SELECT id, job_id, COALESCE(job_group_id, ''), event_type, COALESCE(status, ''), COALESCE(worker_id, ''), COALESCE(metric_name, ''), metric_value, COALESCE(payload, ''), ` + s.timeExpr("created_at") + ` FROM job_events WHERE job_id = ? ORDER BY created_at ASC, id ASC`
	args := []any{jobID}
	if s.handle.Provider == "postgres" {
		query = `SELECT id, job_id, COALESCE(job_group_id, ''), event_type, COALESCE(status, ''), COALESCE(worker_id, ''), COALESCE(metric_name, ''), metric_value, COALESCE(payload::text, ''), ` + s.timeExpr("created_at") + ` FROM job_events WHERE job_id = $1 ORDER BY created_at ASC, id ASC`
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) ListLocks(ctx context.Context, jobID string) ([]Lock, error) {
	query := `SELECT id, lock_key, job_id, owner, status, created_at, expires_at, COALESCE(released_at, '') FROM job_locks WHERE job_id = ? ORDER BY created_at ASC, id ASC`
	args := []any{jobID}
	if s.handle.Provider == "postgres" {
		query = `SELECT id, lock_key, job_id, owner, status, created_at::text, expires_at::text, COALESCE(released_at::text, '') FROM job_locks WHERE job_id = $1 ORDER BY created_at ASC, id ASC`
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lock
	for rows.Next() {
		item, err := scanLock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) findByIdempotency(ctx context.Context, actor, jobType, key string) (Job, error) {
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE actor = ? AND job_type = ? AND idempotency_key = ?"
	args := []any{actor, jobType, key}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE actor = $1 AND job_type = $2 AND idempotency_key = $3"
	}
	job, err := scanJob(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

func (s *Store) findActiveByLock(ctx context.Context, jobType, lockKey string) (Job, error) {
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE job_type = ? AND lock_key = ? AND status IN ('queued', 'running') ORDER BY created_at ASC, id ASC LIMIT 1"
	args := []any{jobType, lockKey}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE job_type = $1 AND lock_key = $2 AND status IN ('queued', 'running') ORDER BY created_at ASC, id ASC LIMIT 1"
	}
	job, err := scanJob(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

func (s *Store) findActiveRepositoryOperation(ctx context.Context, lockKey string) (Job, error) {
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE job_type IN ('repo_clone', 'repo_pull', 'repo_sync') AND lock_key = ? AND status IN ('queued', 'running') ORDER BY created_at ASC, id ASC LIMIT 1"
	args := []any{lockKey}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE job_type IN ('repo_clone', 'repo_pull', 'repo_sync') AND lock_key = $1 AND status IN ('queued', 'running') ORDER BY created_at ASC, id ASC LIMIT 1"
	}
	job, err := scanJob(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

func (s *Store) jobSelectColumns() string {
	if s.handle.Provider == "postgres" {
		return `id, job_type, status, COALESCE(actor, ''), COALESCE(correlation_id, ''), COALESCE(idempotency_key, ''), COALESCE(parent_job_id, ''), COALESCE(job_group_id, ''), COALESCE(lock_key, ''), attempt_count, max_attempts, COALESCE(leased_by, ''), COALESCE(lease_expires_at::text, ''), COALESCE(heartbeat_at::text, ''), run_after::text, priority, payload::text, COALESCE(result_payload::text, ''), created_at::text, COALESCE(started_at::text, ''), COALESCE(finished_at::text, ''), COALESCE(error_message, ''), updated_at::text`
	}
	return `id, job_type, status, COALESCE(actor, ''), COALESCE(correlation_id, ''), COALESCE(idempotency_key, ''), COALESCE(parent_job_id, ''), COALESCE(job_group_id, ''), COALESCE(lock_key, ''), attempt_count, max_attempts, COALESCE(leased_by, ''), COALESCE(lease_expires_at, ''), COALESCE(heartbeat_at, ''), run_after, priority, payload, COALESCE(result_payload, ''), created_at, COALESCE(started_at, ''), COALESCE(finished_at, ''), COALESCE(error_message, ''), updated_at`
}

func scanJob(row interface{ Scan(dest ...any) error }) (Job, error) {
	var job Job
	var lease, heartbeat, runAfter, created, started, finished, updated string
	var payload, result string
	err := row.Scan(&job.ID, &job.JobType, &job.Status, &job.Actor, &job.CorrelationID, &job.IdempotencyKey, &job.ParentJobID, &job.JobGroupID, &job.LockKey, &job.AttemptCount, &job.MaxAttempts, &job.LeasedBy, &lease, &heartbeat, &runAfter, &job.Priority, &payload, &result, &created, &started, &finished, &job.ErrorMessage, &updated)
	if err != nil {
		return Job{}, err
	}
	job.Payload = json.RawMessage(payload)
	if result != "" {
		job.ResultPayload = json.RawMessage(result)
	}
	job.RunAfter, _ = parseTime(runAfter)
	job.CreatedAt, _ = parseTime(created)
	job.UpdatedAt, _ = parseTime(updated)
	job.LeaseExpiresAt = parseTimePtr(lease)
	job.HeartbeatAt = parseTimePtr(heartbeat)
	job.StartedAt = parseTimePtr(started)
	job.FinishedAt = parseTimePtr(finished)
	return job, nil
}

func scanEvent(row interface{ Scan(dest ...any) error }) (Event, error) {
	var ev Event
	var metric sql.NullFloat64
	var payload, created string
	err := row.Scan(&ev.ID, &ev.JobID, &ev.JobGroupID, &ev.EventType, &ev.Status, &ev.WorkerID, &ev.MetricName, &metric, &payload, &created)
	if err != nil {
		return Event{}, err
	}
	if metric.Valid {
		ev.MetricValue = &metric.Float64
	}
	if payload != "" {
		ev.Payload = json.RawMessage(payload)
	}
	ev.CreatedAt, _ = parseTime(created)
	return ev, nil
}

func scanLock(row interface{ Scan(dest ...any) error }) (Lock, error) {
	var item Lock
	var created, expires, released string
	if err := row.Scan(&item.ID, &item.LockKey, &item.JobID, &item.Owner, &item.Status, &created, &expires, &released); err != nil {
		return Lock{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.ExpiresAt, _ = parseTime(expires)
	item.ReleasedAt = parseTimePtr(released)
	return item, nil
}

func (s *Store) placeholder(idx int) string {
	if s.handle.Provider == "postgres" {
		return fmt.Sprintf("$%d", idx)
	}
	return "?"
}

func (s *Store) timeExpr(column string) string {
	if s.handle.Provider == "postgres" {
		return column + "::text"
	}
	return column
}

func defaultJobGroupID(jobType, id string) string {
	switch jobType {
	case "config_reload":
		return "config_operation:" + id
	case "module_restart":
		return "module_operation:" + id
	default:
		return jobType + ":" + id
	}
}

func workflowIDForJob(job Job) string {
	return workflowIDFromGroupID(job.JobGroupID, job.ID)
}

func workflowIDFromGroupID(jobGroupID, fallback string) string {
	if strings.Contains(jobGroupID, ":") {
		parts := strings.SplitN(jobGroupID, ":", 2)
		if parts[1] != "" {
			return parts[1]
		}
	}
	return fallback
}

func workflowTypeForJobType(jobType string) string {
	switch jobType {
	case "config_reload":
		return "config_operation"
	case "module_restart":
		return "module_operation"
	case "project_scan", "security_validation_scan":
		return "project_scan"
	case "repo_clone", "repo_pull", "repo_sync":
		return "repository_operation"
	case "scim_sync":
		return "scim_sync"
	case "global_scan":
		return "global_scan"
	default:
		return jobType
	}
}

func validatePayloadSchema(jobType string, payload json.RawMessage) error {
	expected := payloadSchemaForJobType(jobType)
	if expected == "" {
		return nil
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("invalid job payload: %w", err)
	}
	if envelope.SchemaVersion != expected {
		return fmt.Errorf("invalid job payload schema_version for %s: got %q want %q", jobType, envelope.SchemaVersion, expected)
	}
	return nil
}

func validatePayloadContract(jobType string, payload json.RawMessage) error {
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return fmt.Errorf("invalid job payload: %w", err)
	}
	requiredString := func(key string) error {
		raw, ok := value[key]
		if !ok {
			return fmt.Errorf("invalid job payload for %s: %s is required", jobType, key)
		}
		text, ok := raw.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("invalid job payload for %s: %s must be a non-empty string", jobType, key)
		}
		return nil
	}
	optionalBoolFalse := func(key string) error {
		raw, ok := value[key]
		if !ok || raw == nil {
			return nil
		}
		flag, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("invalid job payload for %s: %s must be a boolean", jobType, key)
		}
		if flag {
			return fmt.Errorf("invalid job payload for %s: %s=true is not supported", jobType, key)
		}
		return nil
	}
	optionalStringArray := func(key string) error {
		raw, ok := value[key]
		if !ok || raw == nil {
			return nil
		}
		items, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("invalid job payload for %s: %s must be an array", jobType, key)
		}
		for _, item := range items {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("invalid job payload for %s: %s must contain only non-empty strings", jobType, key)
			}
		}
		return nil
	}
	oneOfString := func(key string, allowed ...string) error {
		if err := requiredString(key); err != nil {
			return err
		}
		text := strings.TrimSpace(value[key].(string))
		for _, candidate := range allowed {
			if text == candidate {
				return nil
			}
		}
		return fmt.Errorf("invalid job payload for %s: unsupported %s %q", jobType, key, text)
	}

	switch jobType {
	case "global_scan":
		if err := optionalStringArray("root_path_ids"); err != nil {
			return err
		}
		return optionalBoolFalse("follow_symlinks")
	case "project_discovery":
		for _, key := range []string{"project_id", "root_path_id", "relative_path"} {
			if err := requiredString(key); err != nil {
				return err
			}
		}
	case "project_scan":
		for _, key := range []string{"project_id", "project_scan_id", "scan_type"} {
			if err := requiredString(key); err != nil {
				return err
			}
		}
	case "security_validation_scan":
		for _, key := range []string{"project_id", "project_scan_id"} {
			if err := requiredString(key); err != nil {
				return err
			}
		}
		if err := optionalStringArray("enabled_modules"); err != nil {
			return err
		}
	case "repo_clone":
		for _, key := range []string{"provider", "protocol", "clone_scope", "full_path"} {
			if err := requiredString(key); err != nil {
				return err
			}
		}
		if err := oneOfString("clone_scope", "single_repository"); err != nil {
			return err
		}
		if value["root_path_id"] == nil && value["new_root_path"] == nil {
			return fmt.Errorf("invalid job payload for %s: root_path_id or new_root_path is required", jobType)
		}
		if value["target_directory"] == nil && value["new_target_directory"] == nil {
			return fmt.Errorf("invalid job payload for %s: target_directory or new_target_directory is required", jobType)
		}
	case "repo_pull", "repo_sync":
		if err := requiredString("repository_id"); err != nil {
			return err
		}
	case "config_reload":
		return optionalStringArray("keys")
	case "module_restart":
		if err := requiredString("module_name"); err != nil {
			return err
		}
	case "scim_sync":
		if err := requiredString("provider"); err != nil {
			return err
		}
	}
	return nil
}

func payloadSchemaForJobType(jobType string) string {
	switch jobType {
	case "global_scan":
		return "jobs.global_scan.payload.v1"
	case "project_discovery":
		return "jobs.project_discovery.payload.v1"
	case "project_scan":
		return "jobs.project_scan.payload.v1"
	case "security_validation_scan":
		return "jobs.security_validation_scan.payload.v1"
	case "repo_clone":
		return "jobs.repo_clone.payload.v1"
	case "repo_pull":
		return "jobs.repo_pull.payload.v1"
	case "repo_sync":
		return "jobs.repo_sync.payload.v1"
	case "config_reload":
		return "jobs.config_reload.payload.v1"
	case "module_restart":
		return "jobs.module_restart.payload.v1"
	case "scim_sync":
		return "jobs.scim_sync.payload.v1"
	default:
		return ""
	}
}

func nullEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func sameJSON(left, right json.RawMessage) (bool, error) {
	leftCanonical, err := canonicalJSON(left)
	if err != nil {
		return false, err
	}
	rightCanonical, err := canonicalJSON(right)
	if err != nil {
		return false, err
	}
	return leftCanonical == rightCanonical, nil
}

func canonicalJSON(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("invalid job payload: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func eventPayload(message string) json.RawMessage {
	return eventPayloadDetails(message, map[string]any{})
}

func eventPayloadDetails(message string, details map[string]any) json.RawMessage {
	if details == nil {
		details = map[string]any{}
	}
	data, _ := json.Marshal(map[string]any{"schema_version": "job_events.payload.v1", "message": safeMessage(message), "details": details})
	return data
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "duplicate key value") ||
		strings.Contains(message, "constraint failed: unique") ||
		strings.Contains(message, "sqlite_constraint_unique")
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTimePtr(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	t, err := parseTime(value)
	if err != nil {
		return nil
	}
	return &t
}

func parseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07", value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999Z07:00", value); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, value)
}

type listCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func encodeCursor(t time.Time, id string) string {
	payload, _ := json.Marshal(listCursor{Time: t.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(value string) (listCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	var cursor listCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	if cursor.Time.IsZero() || cursor.ID == "" {
		return listCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func newID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
