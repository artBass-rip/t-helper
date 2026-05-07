package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type RuntimeStatus struct {
	AggregateStatus string         `json:"aggregate_status"`
	Jobs            map[string]int `json:"jobs"`
	Workers         map[string]int `json:"workers"`
	Modules         map[string]int `json:"modules"`
	UpdatedAt       time.Time      `json:"updated_at"`
	SchemaVersion   string         `json:"schema_version"`
}

type JobStatus struct {
	JobID          string     `json:"job_id"`
	JobType        string     `json:"job_type"`
	Status         string     `json:"status"`
	JobGroupID     string     `json:"job_group_id"`
	AttemptCount   int        `json:"attempt_count"`
	MaxAttempts    int        `json:"max_attempts"`
	LeasedBy       string     `json:"leased_by,omitempty"`
	HeartbeatAt    *time.Time `json:"heartbeat_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	LatestEvent    any        `json:"latest_event"`
	UpdatedAt      time.Time  `json:"updated_at"`
	SchemaVersion  string     `json:"schema_version"`
}

type WorkerStatus struct {
	WorkerID        string     `json:"worker_id"`
	Status          string     `json:"status"`
	RunningJobID    string     `json:"running_job_id"`
	RunningJobType  string     `json:"running_job_type"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	SchemaVersion   string     `json:"schema_version"`
}

func (s *Store) RefreshWorkflowStatus(ctx context.Context, jobGroupID, workflowID string) error {
	if jobGroupID == "" {
		return nil
	}
	jobs, err := s.list(ctx, ListFilters{JobGroupID: jobGroupID, Limit: 1000}, 1000)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	if workflowID == "" {
		workflowID = workflowIDForJob(jobs[0])
	}
	workflowType := workflowTypeForJobType(jobs[0].JobType)
	counts := map[string]int{StatusQueued: 0, StatusRunning: 0, StatusSucceeded: 0, StatusFailed: 0, StatusCancelled: 0}
	for _, job := range jobs {
		counts[job.Status]++
	}
	aggregate := aggregateStatus(counts, len(jobs))
	progressCurrent := counts[StatusSucceeded] + counts[StatusFailed] + counts[StatusCancelled]
	latest, err := s.latestEventForGroup(ctx, jobGroupID)
	if err != nil {
		return err
	}
	summary := map[string]any{
		"schema_version":   WorkflowSummaryVersion,
		"workflow_type":    workflowType,
		"workflow_id":      workflowID,
		"job_group_id":     jobGroupID,
		"aggregate_status": aggregate,
		"counts":           counts,
		"latest_event":     latestEventSummary(latest),
		"components":       map[string]any{},
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	id := "workflow_" + jobGroupID
	query := `INSERT INTO workflow_statuses (id, workflow_type, workflow_id, job_group_id, aggregate_status, progress_current, progress_total, summary_payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (job_group_id) DO UPDATE SET workflow_type = excluded.workflow_type, workflow_id = excluded.workflow_id, aggregate_status = excluded.aggregate_status, progress_current = excluded.progress_current, progress_total = excluded.progress_total, summary_payload = excluded.summary_payload, updated_at = excluded.updated_at`
	args := []any{id, workflowType, workflowID, jobGroupID, aggregate, progressCurrent, len(jobs), string(payload), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO workflow_statuses (id, workflow_type, workflow_id, job_group_id, aggregate_status, progress_current, progress_total, summary_payload, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (job_group_id) DO UPDATE SET workflow_type = EXCLUDED.workflow_type, workflow_id = EXCLUDED.workflow_id, aggregate_status = EXCLUDED.aggregate_status, progress_current = EXCLUDED.progress_current, progress_total = EXCLUDED.progress_total, summary_payload = EXCLUDED.summary_payload, updated_at = EXCLUDED.updated_at`
	}
	_, err = s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ReconcileWorkflowStatuses(ctx context.Context) error {
	rows, err := s.handle.DB.QueryContext(ctx, "SELECT DISTINCT job_group_id FROM jobs WHERE job_group_id IS NOT NULL AND job_group_id <> ''")
	if err != nil {
		return err
	}
	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, group)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, group := range groups {
		if err := s.RefreshWorkflowStatus(ctx, group, ""); err != nil {
			return err
		}
	}
	return nil
}

type RetentionCleanupResult struct {
	DeletedJobEvents int `json:"deleted_job_events"`
	DeletedJobLocks  int `json:"deleted_job_locks"`
}

func (s *Store) CleanupRetention(ctx context.Context, cutoff time.Time) (RetentionCleanupResult, error) {
	var result RetentionCleanupResult
	eventQuery := "DELETE FROM job_events WHERE created_at < ?"
	lockQuery := "DELETE FROM job_locks WHERE status IN ('released', 'expired') AND COALESCE(released_at, expires_at, created_at) < ?"
	args := []any{formatTime(cutoff)}
	if s.handle.Provider == "postgres" {
		eventQuery = "DELETE FROM job_events WHERE created_at < $1"
		lockQuery = "DELETE FROM job_locks WHERE status IN ('released', 'expired') AND COALESCE(released_at, expires_at, created_at) < $1"
	}
	res, err := s.handle.DB.ExecContext(ctx, eventQuery, args...)
	if err != nil {
		return result, err
	}
	if n, err := res.RowsAffected(); err == nil {
		result.DeletedJobEvents = int(n)
	}
	res, err = s.handle.DB.ExecContext(ctx, lockQuery, args...)
	if err != nil {
		return result, err
	}
	if n, err := res.RowsAffected(); err == nil {
		result.DeletedJobLocks = int(n)
	}
	return result, nil
}

func (s *Store) RuntimeStatus(ctx context.Context) (RuntimeStatus, error) {
	status := RuntimeStatus{
		AggregateStatus: "running",
		Jobs:            map[string]int{StatusQueued: 0, StatusRunning: 0, StatusFailed: 0},
		Workers:         map[string]int{"active": 0, "stale": 0},
		Modules:         map[string]int{},
		UpdatedAt:       time.Now().UTC(),
		SchemaVersion:   "runtime_status.v1",
	}
	rows, err := s.handle.DB.QueryContext(ctx, "SELECT status, count(*) FROM jobs GROUP BY status")
	if err != nil {
		return RuntimeStatus{}, err
	}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			rows.Close()
			return RuntimeStatus{}, err
		}
		status.Jobs[key] = count
	}
	if err := rows.Close(); err != nil {
		return RuntimeStatus{}, err
	}
	workers, err := s.WorkerStatuses(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	for _, worker := range workers {
		status.Workers[worker.Status]++
	}
	moduleRows, err := s.handle.DB.QueryContext(ctx, "SELECT state, count(*) FROM module_states GROUP BY state")
	if err != nil {
		return RuntimeStatus{}, err
	}
	defer moduleRows.Close()
	for moduleRows.Next() {
		var key string
		var count int
		if err := moduleRows.Scan(&key, &count); err != nil {
			return RuntimeStatus{}, err
		}
		status.Modules[key] = count
	}
	if status.Jobs[StatusFailed] > 0 || status.Workers["stale"] > 0 || status.Modules["failed"] > 0 {
		status.AggregateStatus = "degraded"
	}
	return status, moduleRows.Err()
}

func (s *Store) JobStatus(ctx context.Context, id string) (JobStatus, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return JobStatus{}, err
	}
	ev, err := s.LatestEvent(ctx, id)
	if err != nil {
		return JobStatus{}, err
	}
	return JobStatus{
		JobID:          job.ID,
		JobType:        job.JobType,
		Status:         job.Status,
		JobGroupID:     job.JobGroupID,
		AttemptCount:   job.AttemptCount,
		MaxAttempts:    job.MaxAttempts,
		LeasedBy:       job.LeasedBy,
		HeartbeatAt:    job.HeartbeatAt,
		LeaseExpiresAt: job.LeaseExpiresAt,
		LatestEvent:    latestEventSummary(ev),
		UpdatedAt:      job.UpdatedAt,
		SchemaVersion:  "job_status.v1",
	}, nil
}

func (s *Store) WorkerStatuses(ctx context.Context) ([]WorkerStatus, error) {
	now := time.Now().UTC()
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE status = 'running' AND leased_by IS NOT NULL ORDER BY leased_by, updated_at DESC"
	rows, err := s.handle.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []WorkerStatus
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		if seen[job.LeasedBy] {
			continue
		}
		seen[job.LeasedBy] = true
		state := "stale"
		if job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(now) {
			state = "active"
		}
		out = append(out, WorkerStatus{
			WorkerID:        job.LeasedBy,
			Status:          state,
			RunningJobID:    job.ID,
			RunningJobType:  job.JobType,
			LastHeartbeatAt: job.HeartbeatAt,
			LeaseExpiresAt:  job.LeaseExpiresAt,
			SchemaVersion:   "worker_status.v1",
		})
	}
	return out, rows.Err()
}

func (s *Store) WorkflowStatuses(ctx context.Context, workflowType, aggregateStatus string, limit int) ([]WorkflowStatus, error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if workflowType != "" {
		add("workflow_type = %s", workflowType)
	}
	if aggregateStatus != "" {
		add("aggregate_status = %s", aggregateStatus)
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := "SELECT " + s.workflowSelectColumns() + " FROM workflow_statuses"
	if len(where) > 0 {
		query += " WHERE " + where[0]
		for _, clause := range where[1:] {
			query += " AND " + clause
		}
	}
	args = append(args, limit)
	query += " ORDER BY updated_at DESC LIMIT " + s.placeholder(len(args))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowStatus
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) WorkflowStatus(ctx context.Context, jobGroupID string) (WorkflowStatus, error) {
	query := "SELECT " + s.workflowSelectColumns() + " FROM workflow_statuses WHERE job_group_id = ?"
	args := []any{jobGroupID}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.workflowSelectColumns() + " FROM workflow_statuses WHERE job_group_id = $1"
	}
	item, err := scanWorkflow(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return WorkflowStatus{}, ErrNotFound
	}
	return item, err
}

func (s *Store) latestEventForGroup(ctx context.Context, jobGroupID string) (*Event, error) {
	query := `SELECT id, job_id, COALESCE(job_group_id, ''), event_type, COALESCE(status, ''), COALESCE(worker_id, ''), COALESCE(metric_name, ''), metric_value, COALESCE(payload, ''), ` + s.timeExpr("created_at") + ` FROM job_events WHERE job_group_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`
	args := []any{jobGroupID}
	if s.handle.Provider == "postgres" {
		query = `SELECT id, job_id, COALESCE(job_group_id, ''), event_type, COALESCE(status, ''), COALESCE(worker_id, ''), COALESCE(metric_name, ''), metric_value, COALESCE(payload::text, ''), ` + s.timeExpr("created_at") + ` FROM job_events WHERE job_group_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`
	}
	ev, err := scanEvent(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ev, err
}

func latestEventSummary(ev *Event) any {
	if ev == nil {
		return nil
	}
	return map[string]any{
		"job_id":     ev.JobID,
		"event_type": ev.EventType,
		"status":     ev.Status,
		"worker_id":  ev.WorkerID,
		"created_at": ev.CreatedAt,
	}
}

func aggregateStatus(counts map[string]int, total int) string {
	if counts[StatusRunning] > 0 {
		return StatusRunning
	}
	if counts[StatusQueued] > 0 {
		return StatusQueued
	}
	if counts[StatusSucceeded] == total {
		return StatusSucceeded
	}
	if counts[StatusCancelled] == total {
		return StatusCancelled
	}
	if counts[StatusSucceeded] > 0 {
		return "partial"
	}
	if counts[StatusFailed] > 0 {
		return StatusFailed
	}
	return "partial"
}

func (s *Store) workflowSelectColumns() string {
	if s.handle.Provider == "postgres" {
		return `id, workflow_type, workflow_id, job_group_id, aggregate_status, progress_current, progress_total, summary_payload::text, updated_at::text`
	}
	return `id, workflow_type, workflow_id, job_group_id, aggregate_status, progress_current, progress_total, summary_payload, updated_at`
}

func scanWorkflow(row interface{ Scan(dest ...any) error }) (WorkflowStatus, error) {
	var item WorkflowStatus
	var payload, updated string
	if err := row.Scan(&item.ID, &item.WorkflowType, &item.WorkflowID, &item.JobGroupID, &item.AggregateStatus, &item.ProgressCurrent, &item.ProgressTotal, &payload, &updated); err != nil {
		return WorkflowStatus{}, err
	}
	item.SummaryPayload = json.RawMessage(payload)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}
