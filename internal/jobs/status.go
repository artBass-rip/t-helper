package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const retentionCleanupBatchSize = 500
const reconcileWorkflowBatchSize = 500

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
	RunningJobCount int        `json:"running_job_count"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	SchemaVersion   string     `json:"schema_version"`
}

func (s *Store) RefreshWorkflowStatus(ctx context.Context, jobGroupID, workflowID string) error {
	if jobGroupID == "" {
		return nil
	}
	sample, err := s.firstWorkflowJob(ctx, jobGroupID)
	if err != nil {
		return err
	}
	if sample.ID == "" {
		return nil
	}
	if workflowID == "" {
		workflowID = workflowIDForJob(sample)
	}
	workflowType := workflowTypeForJobType(sample.JobType)
	counts := map[string]int{StatusQueued: 0, StatusRunning: 0, StatusSucceeded: 0, StatusFailed: 0, StatusCancelled: 0}
	total := 0
	query := "SELECT status, count(*) FROM jobs WHERE job_group_id = ? GROUP BY status"
	args := []any{jobGroupID}
	if s.handle.Provider == "postgres" {
		query = "SELECT status, count(*) FROM jobs WHERE job_group_id = $1 GROUP BY status"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return err
		}
		counts[status] = count
		total += count
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	aggregate := aggregateStatus(counts, total)
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
	query = `INSERT INTO workflow_statuses (id, workflow_type, workflow_id, job_group_id, aggregate_status, progress_current, progress_total, summary_payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (job_group_id) DO UPDATE SET workflow_type = excluded.workflow_type, workflow_id = excluded.workflow_id, aggregate_status = excluded.aggregate_status, progress_current = excluded.progress_current, progress_total = excluded.progress_total, summary_payload = excluded.summary_payload, updated_at = excluded.updated_at`
	args = []any{id, workflowType, workflowID, jobGroupID, aggregate, progressCurrent, total, string(payload), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO workflow_statuses (id, workflow_type, workflow_id, job_group_id, aggregate_status, progress_current, progress_total, summary_payload, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (job_group_id) DO UPDATE SET workflow_type = EXCLUDED.workflow_type, workflow_id = EXCLUDED.workflow_id, aggregate_status = EXCLUDED.aggregate_status, progress_current = EXCLUDED.progress_current, progress_total = EXCLUDED.progress_total, summary_payload = EXCLUDED.summary_payload, updated_at = EXCLUDED.updated_at`
	}
	_, err = s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) firstWorkflowJob(ctx context.Context, jobGroupID string) (Job, error) {
	query := "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE job_group_id = ? ORDER BY created_at ASC, id ASC LIMIT 1"
	args := []any{jobGroupID}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.jobSelectColumns() + " FROM jobs WHERE job_group_id = $1 ORDER BY created_at ASC, id ASC LIMIT 1"
	}
	job, err := scanJob(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, nil
	}
	return job, err
}

func (s *Store) ReconcileWorkflowStatuses(ctx context.Context) error {
	var after string
	for {
		groups, err := s.workflowGroupsPage(ctx, after, reconcileWorkflowBatchSize)
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			return nil
		}
		for _, group := range groups {
			if err := s.RefreshWorkflowStatus(ctx, group, ""); err != nil {
				return err
			}
		}
		if len(groups) < reconcileWorkflowBatchSize {
			return nil
		}
		after = groups[len(groups)-1]
	}
}

func (s *Store) workflowGroupsPage(ctx context.Context, after string, limit int) ([]string, error) {
	var args []any
	query := "SELECT DISTINCT job_group_id FROM jobs WHERE job_group_id IS NOT NULL AND job_group_id <> ''"
	if after != "" {
		args = append(args, after)
		query += " AND job_group_id > " + s.placeholder(len(args))
	}
	args = append(args, limit)
	query += " ORDER BY job_group_id ASC LIMIT " + s.placeholder(len(args))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

type RetentionCleanupResult struct {
	DeletedJobEvents int `json:"deleted_job_events"`
	DeletedJobLocks  int `json:"deleted_job_locks"`
}

func (s *Store) CleanupRetention(ctx context.Context, cutoff time.Time) (RetentionCleanupResult, error) {
	var result RetentionCleanupResult
	for {
		deleted, err := s.cleanupRetentionBatch(ctx, "job_events", cutoff)
		if err != nil {
			return result, err
		}
		result.DeletedJobEvents += deleted
		if deleted < retentionCleanupBatchSize {
			break
		}
	}
	for {
		deleted, err := s.cleanupRetentionBatch(ctx, "job_locks", cutoff)
		if err != nil {
			return result, err
		}
		result.DeletedJobLocks += deleted
		if deleted < retentionCleanupBatchSize {
			break
		}
	}
	return result, nil
}

func (s *Store) cleanupRetentionBatch(ctx context.Context, table string, cutoff time.Time) (int, error) {
	selectQuery := ""
	args := []any{formatTime(cutoff), retentionCleanupBatchSize}
	switch table {
	case "job_events":
		selectQuery = `SELECT job_events.id
FROM job_events
JOIN jobs ON jobs.id = job_events.job_id
WHERE job_events.created_at < ? AND jobs.status IN ('succeeded', 'failed', 'cancelled')
ORDER BY job_events.created_at ASC, job_events.id ASC
LIMIT ?`
	case "job_locks":
		selectQuery = `SELECT id
FROM job_locks
WHERE status IN ('released', 'expired') AND COALESCE(released_at, expires_at, created_at) < ?
ORDER BY COALESCE(released_at, expires_at, created_at) ASC, id ASC
LIMIT ?`
	default:
		return 0, fmt.Errorf("unsupported retention cleanup table %q", table)
	}
	if s.handle.Provider == "postgres" {
		selectQuery = strings.ReplaceAll(selectQuery, "?", "%s")
		selectQuery = fmt.Sprintf(selectQuery, "$1", "$2")
	}
	rows, err := s.handle.DB.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	deleteArgs := make([]any, len(ids))
	for i, id := range ids {
		deleteArgs[i] = id
	}
	deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", table, s.dialect().InList(1, len(ids)))
	res, err := s.handle.DB.ExecContext(ctx, deleteQuery, deleteArgs...)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return len(ids), nil
	}
	return int(affected), nil
}

func (s *Store) RuntimeStatus(ctx context.Context) (RuntimeStatus, error) {
	status := RuntimeStatus{
		AggregateStatus: "running",
		Jobs:            map[string]int{StatusQueued: 0, StatusRunning: 0, StatusSucceeded: 0, StatusFailed: 0, StatusCancelled: 0},
		Workers:         map[string]int{"active": 0, "stale": 0},
		Modules:         map[string]int{"running": 0, "stopped": 0, "failed": 0, "unavailable": 0},
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
	if status.Modules["failed"] > 0 && status.Modules["running"] == 0 {
		status.AggregateStatus = "failed"
	} else if status.Jobs[StatusFailed] > 0 || status.Workers["stale"] > 0 || status.Modules["failed"] > 0 {
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
	var out []WorkerStatus
	cursor := ""
	for {
		page, err := s.WorkerStatusesPage(ctx, 200, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if page.NextCursor == "" {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

func (s *Store) WorkerStatusesPage(ctx context.Context, limit int, cursorValue string) (Page[WorkerStatus], error) {
	now := time.Now().UTC()
	var where []string
	var args []any
	where = append(where, "j.status = 'running'", "j.leased_by IS NOT NULL", "j.leased_by <> ''", `NOT EXISTS (
		SELECT 1 FROM jobs newer
		WHERE newer.status = 'running'
		  AND newer.leased_by = j.leased_by
		  AND (newer.updated_at > j.updated_at OR (newer.updated_at = j.updated_at AND newer.id > j.id))
	)`)
	if cursorValue != "" {
		cursor, err := decodeCursor(cursorValue)
		if err != nil {
			return Page[WorkerStatus]{}, err
		}
		args = append(args, formatTime(cursor.Time), formatTime(cursor.Time), cursor.ID)
		where = append(where, fmt.Sprintf("(j.updated_at < %s OR (j.updated_at = %s AND j.id < %s))", s.placeholder(len(args)-2), s.placeholder(len(args)-1), s.placeholder(len(args))))
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	countExpr := "(SELECT count(*) FROM jobs counted WHERE counted.status = 'running' AND counted.leased_by = j.leased_by)"
	query := "SELECT j.id, j.job_type, COALESCE(j.leased_by, ''), COALESCE(" + s.timeExpr("j.heartbeat_at") + ", ''), COALESCE(" + s.timeExpr("j.lease_expires_at") + ", ''), " + s.timeExpr("j.updated_at") + ", " + countExpr + " FROM jobs j WHERE " + strings.Join(where, " AND ") + " ORDER BY j.updated_at DESC, j.id DESC LIMIT " + s.placeholder(len(args)+1)
	args = append(args, limit+1)
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[WorkerStatus]{}, err
	}
	defer rows.Close()
	var out []WorkerStatus
	var cursors []listCursor
	for rows.Next() {
		job, updatedAt, runningCount, err := scanWorkerStatusJob(rows)
		if err != nil {
			return Page[WorkerStatus]{}, err
		}
		state := "stale"
		if job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(now) {
			state = "active"
		}
		out = append(out, WorkerStatus{
			WorkerID:        job.LeasedBy,
			Status:          state,
			RunningJobID:    job.ID,
			RunningJobType:  job.JobType,
			RunningJobCount: runningCount,
			LastHeartbeatAt: job.HeartbeatAt,
			LeaseExpiresAt:  job.LeaseExpiresAt,
			SchemaVersion:   "worker_status.v1",
		})
		cursors = append(cursors, listCursor{Time: updatedAt, ID: job.ID})
	}
	if err := rows.Err(); err != nil {
		return Page[WorkerStatus]{}, err
	}
	var next string
	if len(out) > limit {
		out = out[:limit]
		last := cursors[limit-1]
		next = encodeCursor(last.Time, last.ID)
	}
	return Page[WorkerStatus]{Items: out, NextCursor: next}, nil
}

func scanWorkerStatusJob(row interface{ Scan(dest ...any) error }) (Job, time.Time, int, error) {
	var job Job
	var heartbeat, lease, updated string
	var runningCount int
	if err := row.Scan(&job.ID, &job.JobType, &job.LeasedBy, &heartbeat, &lease, &updated, &runningCount); err != nil {
		return Job{}, time.Time{}, 0, err
	}
	job.HeartbeatAt = parseTimePtr(heartbeat)
	job.LeaseExpiresAt = parseTimePtr(lease)
	updatedAt, _ := parseTime(updated)
	return job, updatedAt, runningCount, nil
}

func (s *Store) WorkflowStatuses(ctx context.Context, workflowType, aggregateStatus string, limit int) ([]WorkflowStatus, error) {
	page, err := s.WorkflowStatusesPage(ctx, workflowType, aggregateStatus, limit, "")
	return page.Items, err
}

func (s *Store) WorkflowStatusesPage(ctx context.Context, workflowType, aggregateStatus string, limit int, cursorValue string) (Page[WorkflowStatus], error) {
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
	if cursorValue != "" {
		cursor, err := decodeCursor(cursorValue)
		if err != nil {
			return Page[WorkflowStatus]{}, err
		}
		args = append(args, formatTime(cursor.Time), formatTime(cursor.Time), cursor.ID)
		where = append(where, fmt.Sprintf("(updated_at < %s OR (updated_at = %s AND id < %s))", s.placeholder(len(args)-2), s.placeholder(len(args)-1), s.placeholder(len(args))))
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
	args = append(args, limit+1)
	query += " ORDER BY updated_at DESC, id DESC LIMIT " + s.placeholder(len(args))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[WorkflowStatus]{}, err
	}
	defer rows.Close()
	var out []WorkflowStatus
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return Page[WorkflowStatus]{}, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return Page[WorkflowStatus]{}, err
	}
	var next string
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(last.UpdatedAt, last.ID)
	}
	return Page[WorkflowStatus]{Items: out, NextCursor: next}, nil
}

func (s *Store) WorkflowStatus(ctx context.Context, jobGroupID string) (WorkflowStatus, error) {
	item, err := s.workflowStatus(ctx, jobGroupID)
	if errors.Is(err, ErrNotFound) {
		if refreshErr := s.RefreshWorkflowStatus(ctx, jobGroupID, ""); refreshErr != nil {
			return WorkflowStatus{}, refreshErr
		}
		return s.workflowStatus(ctx, jobGroupID)
	}
	return item, err
}

func (s *Store) workflowStatus(ctx context.Context, jobGroupID string) (WorkflowStatus, error) {
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
	summary := map[string]any{
		"job_id":     ev.JobID,
		"event_type": ev.EventType,
		"status":     ev.Status,
		"worker_id":  ev.WorkerID,
		"created_at": ev.CreatedAt,
	}
	var payload struct {
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if len(ev.Payload) > 0 && json.Unmarshal(ev.Payload, &payload) == nil {
		if payload.Message != "" {
			summary["message"] = payload.Message
		}
		if len(payload.Details) > 0 {
			summary["details"] = payload.Details
		}
	}
	return summary
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
