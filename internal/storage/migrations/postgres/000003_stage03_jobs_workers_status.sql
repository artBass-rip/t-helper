-- +goose Up
CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  job_type TEXT NOT NULL CHECK (job_type IN ('global_scan', 'project_discovery', 'project_scan', 'security_validation_scan', 'repo_clone', 'repo_pull', 'repo_sync', 'config_reload', 'module_restart', 'scim_sync')),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
  actor TEXT,
  correlation_id TEXT,
  idempotency_key TEXT,
  parent_job_id TEXT REFERENCES jobs(id) ON DELETE RESTRICT,
  job_group_id TEXT,
  lock_key TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
  leased_by TEXT,
  lease_expires_at TIMESTAMPTZ,
  heartbeat_at TIMESTAMPTZ,
  run_after TIMESTAMPTZ NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  result_payload JSONB,
  created_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error_message TEXT,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (attempt_count <= max_attempts)
);

CREATE UNIQUE INDEX IF NOT EXISTS jobs_idempotency_key_unique
  ON jobs (actor, job_type, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS jobs_parent_job_id_idx ON jobs (parent_job_id);
CREATE INDEX IF NOT EXISTS jobs_group_status_idx ON jobs (job_group_id, status);
CREATE INDEX IF NOT EXISTS jobs_lock_status_idx ON jobs (lock_key, status);
CREATE INDEX IF NOT EXISTS jobs_claim_idx ON jobs (status, run_after ASC, priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS jobs_lease_expires_at_idx ON jobs (lease_expires_at);
CREATE INDEX IF NOT EXISTS jobs_leased_by_status_idx ON jobs (leased_by, status);
CREATE INDEX IF NOT EXISTS jobs_worker_status_idx ON jobs (status, leased_by, updated_at DESC);
CREATE INDEX IF NOT EXISTS jobs_worker_status_page_idx ON jobs (status, updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS job_locks (
  id TEXT PRIMARY KEY,
  lock_key TEXT NOT NULL,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  owner TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('held', 'released', 'expired')),
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  released_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS job_locks_held_lock_key_unique
  ON job_locks (lock_key)
  WHERE status = 'held';

CREATE INDEX IF NOT EXISTS job_locks_job_id_idx ON job_locks (job_id);
CREATE INDEX IF NOT EXISTS job_locks_expires_at_idx ON job_locks (expires_at);
CREATE INDEX IF NOT EXISTS job_locks_cleanup_idx
  ON job_locks (status, released_at, expires_at, created_at, id);

CREATE TABLE IF NOT EXISTS job_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
  job_group_id TEXT,
  event_type TEXT NOT NULL CHECK (event_type IN ('queued', 'claimed', 'started', 'heartbeat', 'progress', 'child_created', 'succeeded', 'failed', 'cancelled', 'lease_expired', 'retry_scheduled')),
  status TEXT,
  worker_id TEXT,
  metric_name TEXT,
  metric_value DOUBLE PRECISION,
  payload JSONB,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS job_events_group_created_at_idx ON job_events (job_group_id, created_at);
CREATE INDEX IF NOT EXISTS job_events_job_created_at_idx ON job_events (job_id, created_at);
CREATE INDEX IF NOT EXISTS job_events_created_at_idx ON job_events (created_at, id);

CREATE TABLE IF NOT EXISTS workflow_statuses (
  id TEXT PRIMARY KEY,
  workflow_type TEXT NOT NULL CHECK (workflow_type IN ('project_scan', 'project_discovery', 'global_scan', 'repository_operation', 'config_operation', 'module_operation', 'scim_sync')),
  workflow_id TEXT NOT NULL,
  job_group_id TEXT NOT NULL,
  aggregate_status TEXT NOT NULL CHECK (aggregate_status IN ('queued', 'running', 'succeeded', 'failed', 'partial', 'cancelled')),
  progress_current INTEGER NOT NULL CHECK (progress_current >= 0),
  progress_total INTEGER NOT NULL CHECK (progress_total >= 0),
  summary_payload JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (progress_current <= progress_total)
);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_statuses_type_id_unique
  ON workflow_statuses (workflow_type, workflow_id);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_statuses_job_group_id_unique
  ON workflow_statuses (job_group_id);

CREATE INDEX IF NOT EXISTS workflow_statuses_updated_at_idx
  ON workflow_statuses (updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS workflow_statuses_filter_updated_at_idx
  ON workflow_statuses (workflow_type, aggregate_status, updated_at DESC, id DESC);

UPDATE system_metadata SET value = 'stage-03', updated_at = now()
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS workflow_statuses_job_group_id_unique;
DROP INDEX IF EXISTS workflow_statuses_filter_updated_at_idx;
DROP INDEX IF EXISTS workflow_statuses_updated_at_idx;
DROP INDEX IF EXISTS workflow_statuses_type_id_unique;
DROP TABLE IF EXISTS workflow_statuses;
DROP INDEX IF EXISTS job_events_created_at_idx;
DROP INDEX IF EXISTS job_events_job_created_at_idx;
DROP INDEX IF EXISTS job_events_group_created_at_idx;
DROP TABLE IF EXISTS job_events;
DROP INDEX IF EXISTS job_locks_expires_at_idx;
DROP INDEX IF EXISTS job_locks_cleanup_idx;
DROP INDEX IF EXISTS job_locks_job_id_idx;
DROP INDEX IF EXISTS job_locks_held_lock_key_unique;
DROP TABLE IF EXISTS job_locks;
DROP INDEX IF EXISTS jobs_leased_by_status_idx;
DROP INDEX IF EXISTS jobs_worker_status_page_idx;
DROP INDEX IF EXISTS jobs_worker_status_idx;
DROP INDEX IF EXISTS jobs_lease_expires_at_idx;
DROP INDEX IF EXISTS jobs_claim_idx;
DROP INDEX IF EXISTS jobs_lock_status_idx;
DROP INDEX IF EXISTS jobs_group_status_idx;
DROP INDEX IF EXISTS jobs_parent_job_id_idx;
DROP INDEX IF EXISTS jobs_idempotency_key_unique;
DROP TABLE IF EXISTS jobs;

UPDATE system_metadata SET value = 'stage-02', updated_at = now()
  WHERE key = 'schema_version';
