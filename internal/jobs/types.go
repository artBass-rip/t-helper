package jobs

import (
	"encoding/json"
	"time"
)

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	EventQueued         = "queued"
	EventClaimed        = "claimed"
	EventStarted        = "started"
	EventHeartbeat      = "heartbeat"
	EventProgress       = "progress"
	EventChildCreated   = "child_created"
	EventSucceeded      = "succeeded"
	EventFailed         = "failed"
	EventCancelled      = "cancelled"
	EventLeaseExpired   = "lease_expired"
	EventRetryScheduled = "retry_scheduled"

	ResultFailureSchemaVersion = "jobs.failure.result.v1"
	WorkflowSummaryVersion     = "workflow_status.summary.v1"
	JobRefSchemaVersion        = "job_ref.v1"
)

type Job struct {
	ID             string          `json:"id"`
	JobType        string          `json:"job_type"`
	Status         string          `json:"status"`
	Actor          string          `json:"actor,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	ParentJobID    string          `json:"parent_job_id,omitempty"`
	JobGroupID     string          `json:"job_group_id,omitempty"`
	LockKey        string          `json:"lock_key,omitempty"`
	AttemptCount   int             `json:"attempt_count"`
	MaxAttempts    int             `json:"max_attempts"`
	LeasedBy       string          `json:"leased_by,omitempty"`
	LeaseExpiresAt *time.Time      `json:"lease_expires_at,omitempty"`
	HeartbeatAt    *time.Time      `json:"heartbeat_at,omitempty"`
	RunAfter       time.Time       `json:"run_after"`
	Priority       int             `json:"priority"`
	Payload        json.RawMessage `json:"payload"`
	ResultPayload  json.RawMessage `json:"result_payload,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Event struct {
	ID          string          `json:"id"`
	JobID       string          `json:"job_id"`
	JobGroupID  string          `json:"job_group_id,omitempty"`
	EventType   string          `json:"event_type"`
	Status      string          `json:"status,omitempty"`
	WorkerID    string          `json:"worker_id,omitempty"`
	MetricName  string          `json:"metric_name,omitempty"`
	MetricValue *float64        `json:"metric_value,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type WorkflowStatus struct {
	ID              string          `json:"id"`
	WorkflowType    string          `json:"workflow_type"`
	WorkflowID      string          `json:"workflow_id"`
	JobGroupID      string          `json:"job_group_id"`
	AggregateStatus string          `json:"aggregate_status"`
	ProgressCurrent int             `json:"progress_current"`
	ProgressTotal   int             `json:"progress_total"`
	SummaryPayload  json.RawMessage `json:"summary_payload"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type JobRef struct {
	JobID         string `json:"job_id"`
	Status        string `json:"status"`
	SchemaVersion string `json:"schema_version"`
}

type EnqueueRequest struct {
	ID             string
	JobType        string
	Actor          string
	CorrelationID  string
	IdempotencyKey string
	ParentJobID    string
	JobGroupID     string
	WorkflowID     string
	LockKey        string
	MaxAttempts    int
	RunAfter       time.Time
	Priority       int
	Payload        json.RawMessage
}

type ClaimOptions struct {
	WorkerID      string
	Now           time.Time
	LeaseDuration time.Duration
}

type FailureResult struct {
	SchemaVersion string `json:"schema_version"`
	JobType       string `json:"job_type"`
	WorkerID      string `json:"worker_id"`
	Attempt       int    `json:"attempt"`
	ErrorCode     string `json:"error_code"`
	Message       string `json:"message"`
	Retryable     bool   `json:"retryable"`
}

type HandlerError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e HandlerError) Error() string {
	return e.Message
}
