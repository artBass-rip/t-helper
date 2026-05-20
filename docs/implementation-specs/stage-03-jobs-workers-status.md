# Stage 03: Jobs, Workers and Status Foundation

## Цель

Реализовать persistent jobs framework, worker execution model, leases, locks и базовый status-monitor.

## Inputs

- `docs/adr/0003-job-worker-processes.md`
- `docs/adr/0005-job-leasing-and-worker-coordination.md`
- `docs/adr/0006-project-scan-workflow-and-status-aggregation.md`
- `docs/adr/0011-worker-defaults.md`
- `docs/data-model.md`
- `docs/payload-schemas.md`
- `docs/api.md`
- `docs/test-plan.md`

## Scope

- `jobs`;
- `job_locks`;
- `job_events`;
- `workflow_statuses`;
- atomic job claim;
- heartbeat, lease expiry recovery, retry/backoff;
- SQLite worker process limit enforcement;
- worker handlers для `config_reload` и `module_restart`;
- basic status-monitor для jobs/workers/modules;
- status API endpoints.

## Implementation decisions

- Stage 03 does not change Stage 02 synchronous contracts for
  `POST /api/modules/reload` and `POST /api/modules/restart`.
- Stage 03 implements `jobs.config_reload` and `jobs.module_restart` worker
  handlers as framework validation handlers and for future workflow
  integration.
- Public write endpoints that are explicitly documented as background
  operations in `docs/api.md` create `jobs` in `queued` and return
  `202 job_ref`.
- Existing Stage 02 synchronous lifecycle endpoints remain explicit exceptions
  until a later stage introduces async variants or a documented contract
  migration.
- Stage 03 does not change `thelper-ctl reload` and
  `thelper-ctl restart <module>` contracts. They remain synchronous Stage 02
  lifecycle commands, return `config_reload.result.v1` /
  `module_restart.result.v1`, do not require a running worker and do not
  enqueue jobs.
- Any async CLI lifecycle behavior must be introduced later as an explicit
  command or flag and must return `job_ref` or provide a documented wait mode.
- `GET /api/status`, `GET /api/status/jobs/{job_id}` and
  `GET /api/status/workers` use the `runtime_status.v1`, `job_status.v1` and
  `worker_status.v1` DTOs from `docs/api.md`.
- Stage 03 derives `worker_status.v1` from running jobs and their leases.
  Idle workers are not reported until a later worker heartbeat registry is
  introduced.
- For SQLite active profiles, Stage 03 enforces `worker_process_limit = 1`
  with a local worker process lock keyed by the active database fingerprint.
  The lock is separate from `job_locks`: it limits local SQLite worker process
  count, while `job_locks` serialize business resources.
- `Idempotency-Key` uniqueness is scoped by operation owner, not global across
  all jobs. Stage 03 stores `idempotency_key` on `jobs` and enforces uniqueness
  on `(actor, job_type, idempotency_key)` for non-null keys. Empty actor is
  normalized to `system` at enqueue time.
- `jobs.payload` is validated before persistence and rejected if it contains
  secret-like JSON keys, URL userinfo or unresolved `secretref://...` values.
- `jobs.payload` is also validated against the minimum admitted job-type
  contract from `docs/payload-schemas.md` before persistence. This prevents a
  job type admitted by the physical schema from being queued with a valid
  `schema_version` but missing required routing/domain fields.

## Worker handler contract

Worker runtime owns job lifecycle transitions, lease heartbeat,
`job_locks` acquire/release, final status writes and base `job_events`.
Handlers are domain executors: they receive an already claimed job, perform the
domain operation and return a result payload or classified error.

The handler registry maps `jobs.job_type` to a handler. Stage 03 registers
handlers for `config_reload` and `module_restart`. Unknown job types fail
without retry using `error_code = "unknown_job_type"`.

Handler input is the normalized persisted job record, not only
`jobs.payload`. At minimum the runtime passes:

- `id`;
- `job_type`;
- `actor`;
- `correlation_id`;
- `idempotency_key`;
- `parent_job_id`;
- `job_group_id`;
- `lock_key`;
- `attempt_count`;
- `max_attempts`;
- `payload`.

Handlers also receive a context and execution environment with `worker_id`,
logger and domain stores. Handlers must be context-cancellable, must pass the
context into storage calls and must not start child processes or long loops
that ignore cancellation.

Transactional boundaries:

- claim, lease update, lock acquire/release, base event writes and final
  `jobs.status` updates are runtime-owned;
- handlers must not set `jobs.status`, `leased_by`, `lease_expires_at`,
  `heartbeat_at`, `started_at` or `finished_at` directly;
- handlers may perform domain storage writes using short transactions;
- neither runtime nor handlers may hold one database transaction open across
  long-running work or while waiting on external processes.

Runtime lifecycle for one claimed job:

1. atomically claim eligible job and set `status = running`;
2. write `claimed` event;
3. acquire `job_locks` when `lock_key` is set;
4. write `started` event and start runtime-owned heartbeat ticker;
5. execute handler;
6. stop heartbeat ticker;
7. save handler result payload or `jobs.failure.result.v1`;
8. release held `job_locks`;
9. mark job `succeeded`, `failed` or requeue with retry/backoff;
10. write final `succeeded`, `failed` or `retry_scheduled` event.

Heartbeat is runtime-owned. Each heartbeat tick updates `jobs.heartbeat_at` and
extends `jobs.lease_expires_at` while the job is still `running` and leased by
the same `worker_id`. Handlers may emit `progress` or `child_created` events
through runtime helpers, but they must not update `heartbeat_at` or extend
leases directly.

Heartbeat `job_events` are diagnostic and must not be emitted on every tick by
default. Stage 03 must write heartbeat facts to `jobs.heartbeat_at`; it may emit
a `heartbeat` event at job start, after stale recovery, or at a bounded
diagnostic cadence. Status freshness and worker health must not depend on a
heartbeat event existing for every heartbeat tick.

Error classification:

- `validation_error`: invalid job payload or unsupported payload value;
  non-retryable, job becomes `failed`;
- `unknown_job_type`: no registered handler; non-retryable, job becomes
  `failed`;
- `lock_contention`: business lock could not be acquired; handler must not run,
  lease is cleared and job returns to `queued` with retry/backoff;
- `transient_error`: temporary storage/runtime/external dependency failure;
  retryable until `max_attempts`;
- `handler_failed`: handler ran and the domain operation failed; retryable
  until `max_attempts` unless the handler marks it non-retryable;
- `cancelled`: context cancellation or explicit job cancellation; Stage 03
  internal shutdown cancellation relies on lease expiry/recovery if the handler
  cannot finish cleanly.

Failed jobs store `jobs.failure.result.v1` unless a job-specific failed result
schema is explicitly documented. Failure result payloads must include
`worker_id`, `attempt`, `error_code` and a safe message, and must not contain
secrets, raw Terraform source, tokens, passwords, private keys or unresolved
secret values.

## Physical migration contract

Stage 03 must add paired SQLite/PostgreSQL migrations with synchronized logical
version `000003`. The migrations create `jobs`, `job_locks`, `job_events` and
`workflow_statuses`, then update `system_metadata.schema_version` to
`stage-03`.

Dialect storage rules:

- SQLite stores timestamps as UTC RFC3339Nano `TEXT`.
- PostgreSQL stores timestamps as `TIMESTAMPTZ`.
- SQLite stores JSON payloads as `TEXT` with `json_valid(...)` checks.
- PostgreSQL stores JSON payloads as `JSONB`.
- JSON columns that represent absent optional data may be nullable; required
  JSON payloads must default to `{}`.

`jobs` required columns:

- `id`;
- `job_type`;
- `status`;
- `attempt_count`;
- `max_attempts`;
- `run_after`;
- `priority`;
- `payload`;
- `created_at`;
- `updated_at`.

`jobs` nullable columns:

- `actor`;
- `correlation_id`;
- `idempotency_key`;
- `parent_job_id`;
- `job_group_id`;
- `lock_key`;
- `leased_by`;
- `lease_expires_at`;
- `heartbeat_at`;
- `result_payload`;
- `started_at`;
- `finished_at`;
- `error_message`.

`jobs` constraints:

- `job_type` must be one of the values listed in `docs/data-model.md`;
- `status` must be one of `queued`, `running`, `succeeded`, `failed` or
  `cancelled`;
- `attempt_count >= 0`;
- `max_attempts > 0`;
- `attempt_count <= max_attempts`;
- `priority >= 0`;
- `payload` must be valid JSON;
- `result_payload` must be valid JSON when non-null;
- `parent_job_id` references `jobs(id)` with `ON DELETE RESTRICT`.

`jobs` indexes:

- partial unique index on `(actor, job_type, idempotency_key)` where
  `idempotency_key IS NOT NULL`;
- index on `parent_job_id`;
- index on `(job_group_id, status)`;
- index on `(lock_key, status)`;
- worker claim index on `(status, run_after, priority, created_at)`;
- index on `lease_expires_at`;
- index on `(leased_by, status)`.
- read-path index for derived worker status on `(status, leased_by, updated_at)`.

Worker claim ordering is `run_after ASC`, `priority DESC`, `created_at ASC`.
The exact SQL may differ by dialect, but the index and query must support this
ordering.

`job_locks` required columns:

- `id`;
- `lock_key`;
- `job_id`;
- `owner`;
- `status`;
- `created_at`;
- `expires_at`.

`job_locks` nullable columns:

- `released_at`.

`job_locks` constraints and indexes:

- `status` must be one of `held`, `released` or `expired`;
- `job_id` references `jobs(id)` with `ON DELETE RESTRICT`;
- `expires_at` is required for held locks;
- partial unique index on `lock_key` where `status = 'held'`;
- index on `job_id`;
- index on `expires_at`.

`job_events` required columns:

- `id`;
- `job_id`;
- `event_type`;
- `created_at`.

`job_events` nullable columns:

- `job_group_id`;
- `status`;
- `worker_id`;
- `metric_name`;
- `metric_value`;
- `payload`.

`job_events` constraints and indexes:

- `event_type` must include at least the baseline values from
  `docs/data-model.md`;
- `payload` must be valid JSON when non-null;
- `job_id` references `jobs(id)` with `ON DELETE RESTRICT`;
- index on `(job_group_id, created_at)`;
- index on `(job_id, created_at)`.

`workflow_statuses` required columns:

- `id`;
- `workflow_type`;
- `workflow_id`;
- `job_group_id`;
- `aggregate_status`;
- `progress_current`;
- `progress_total`;
- `summary_payload`;
- `updated_at`.

`workflow_statuses` constraints and indexes:

- `workflow_type` must include at least the baseline values from
  `docs/data-model.md`;
- `aggregate_status` must be one of `queued`, `running`, `succeeded`, `failed`,
  `partial` or `cancelled`;
- `progress_current >= 0`;
- `progress_total >= 0`;
- `progress_current <= progress_total`;
- `summary_payload` must be valid JSON;
- unique index on `(workflow_type, workflow_id)`;
- unique index on `job_group_id`.
- read-path index on `(updated_at, id)` for paginated workflow status listing;
- read-path index on `(workflow_type, aggregate_status, updated_at, id)` for
  filtered workflow status listing.

Runtime timestamp rules:

- `started_at` is set when a job is successfully claimed and started;
- `finished_at` is set only for terminal statuses;
- completed jobs may retain `leased_by` for diagnostics;
- terminal statuses must not be treated as actively leased even if
  `lease_expires_at` remains populated;
- expired lease recovery only considers `status = 'running'` rows.

## Status-monitor aggregation contract

Stage 03 `status-monitor` owns `workflow_statuses` and the status DTO
aggregation exposed through `/api/status*`. Workers and handlers write facts to
`jobs` and `job_events`; UI and internal services must not compute workflow
status from those facts themselves.

Every enqueued job must have `job_group_id`. For single-job workflows, enqueue
generates the job ID before insert and sets:

- `config_reload`: `job_group_id = config_operation:<job_id>`;
- `module_restart`: `job_group_id = module_operation:<job_id>`;
- fallback: `job_group_id = <job_type>:<job_id>`.

Stage 03 workflow type mapping:

- `config_reload` -> `config_operation`;
- `module_restart` -> `module_operation`;
- `global_scan` -> `global_scan`;
- `project_discovery` -> `project_discovery`;
- `project_scan` and `security_validation_scan` -> `project_scan`;
- `repo_clone`, `repo_pull` and `repo_sync` -> `repository_operation`;
- `scim_sync` -> `scim_sync`;
- future job types require a schema migration before enqueueing; once admitted
  by the physical schema, unrecognized types use `job_type` as fallback
  `workflow_type`.

For single-job workflows, `workflow_id` is the job ID. For grouped workflows
introduced by later stages, the parent/orchestrator creation path owns
`workflow_id`.

Aggregate status is computed from all jobs sharing one `job_group_id` using
this precedence:

1. if any job is `running`, aggregate is `running`;
2. otherwise, if any job is `queued`, aggregate is `queued`;
3. otherwise, if all jobs are `succeeded`, aggregate is `succeeded`;
4. otherwise, if all jobs are `cancelled`, aggregate is `cancelled`;
5. otherwise, if terminal statuses are mixed and at least one job is
   `succeeded`, aggregate is `partial`;
6. otherwise, if any job is `failed`, aggregate is `failed`;
7. otherwise, if terminal statuses are mixed without success, aggregate is
   `partial`.

Active statuses (`running`, then `queued`) take precedence over terminal mixed
states because the workflow is still in progress.

Progress is deterministic:

- `progress_total = count(jobs where job_group_id = ...)`;
- `progress_current = count(jobs in terminal statuses: succeeded, failed,
  cancelled)`;
- single-job queued/running workflows report `0/1`;
- single-job terminal workflows report `1/1`.

Latest event selection:

- `GET /api/status/jobs/{job_id}` uses the latest `job_events` row for that
  job ordered by `created_at DESC, id DESC`;
- `workflow_statuses.summary_payload.latest_event` uses the latest
  `job_events` row for the workflow's `job_group_id` ordered by
  `created_at DESC, id DESC`;
- if no event exists, `latest_event` is `null`.

Refresh behavior:

- after every runtime lifecycle transition, worker runtime calls
  `RefreshWorkflowStatus(job_group_id)`;
- `status-monitor` must also support reconciliation from `jobs` and
  `job_events` so read models can be rebuilt after crashes or missed inline
  refreshes;
- API/server startup and `GET /api/status/workflows` trigger reconciliation so
  workflow listing self-heals after crashes or missed inline refreshes even
  before a worker process starts;
- refresh writes `workflow_statuses` using `workflow_status.summary.v1`.

## Non-goals

- filesystem scanning;
- repository operations;
- project/security scan toolchain;
- auth/RBAC enforcement beyond system contracts.

## Deliverables

- `thelper-worker` execution loop;
- `thelper-worker` storage/config bootstrap using the same storage provider,
  DSN and active storage profile resolution as `thelper`;
- job lease implementation для SQLite/PostgreSQL;
- lock acquire/release semantics;
- status-monitor read models;
- job/status API;
- worker integration tests.

## Definition of Done

- endpoints documented as background operations create jobs in `queued` and do
  not execute long-running work inline;
- Stage 02 synchronous module lifecycle endpoints continue returning their
  synchronous result DTOs without requiring jobs;
- Stage 02 synchronous `thelper-ctl reload` and `thelper-ctl restart <module>`
  commands continue returning their synchronous result DTOs without requiring a
  running worker or creating jobs;
- только один worker может claim один job;
- heartbeat обновляется для running jobs;
- expired leases восстанавливаются через retry/backoff или failure после `max_attempts`;
- `job_locks` не допускают конфликтующие active operations;
- status endpoints читают aggregate read models.

## Remaining MVP blockers

- нет Stage 03 blockers после ADR 0011.

## Implementation verification

Status: completed.

Code coverage:

- Storage and migrations: `internal/storage/migrations/*000003*`,
  `internal/jobs/store.go`, `internal/jobs/status.go`;
- Worker runtime: `internal/jobs/runtime.go`, `internal/app/worker/app.go`;
- HTTP API: `internal/httpapi/jobs.go`, `internal/httpapi/router.go`;
- CLI/API lifecycle compatibility: `internal/app/ctl`, `internal/httpapi`,
  `internal/app/server`.

Verification:

- `go test ./internal/jobs ./internal/httpapi ./internal/app/worker ./internal/storage/migrations`;
- `go test ./...`;
- PostgreSQL jobs/storage contracts run in CI and local environments when
  `THELPER_POSTGRES_DSN` is set.

Regression coverage added for 100% Stage 03 closure:

- lock-contention jobs are requeued without `started_at` and without
  `started` events;
- unknown admitted job types fail without retry using
  `jobs.failure.result.v1` and `error_code = "unknown_job_type"`;
- non-SQLite worker profiles do not require the local SQLite worker process
  lock.
- admitted job payloads are rejected before persistence when required
  job-type fields are missing or unsupported Stage 03 values are requested;
- workflow status listing reconciles missing `workflow_statuses` read models
  before returning results.

## Traceability

- Roadmap: Stage 03.
- Acceptance: `ACC-MVP-009`, `ACC-MVP-010`, `ACC-MVP-014`, `ACC-MVP-021`.
- API: `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/status*`.
- Data model: `jobs`, `job_locks`, `job_events`, `workflow_statuses`.
- ADR: `0003`, `0005`, `0006`, `0011`.

## Риски

- race conditions между workers;
- разные claim semantics в SQLite и PostgreSQL;
- UI начнёт агрегировать статус самостоятельно вместо `status-monitor`.
