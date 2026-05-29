# ADR 0005: Job leasing and worker coordination

## Status

Accepted.

## Decision

`thelper-worker` processes must claim jobs through a storage-level atomic lease before executing them.

Job leasing is separate from `job_locks`:

- job lease controls ownership of a specific `jobs.id`;
- `job_locks` protect business resources such as `repository:<repository_id>`.

## Rationale

Multiple worker processes can see the same queued job. Without an atomic claim/lease mechanism, more than one worker could execute the same job.

`job_locks` are not enough because they serialize domain resources, not ownership of the job row itself.

## Required job fields

The `jobs` entity must include:

- `attempt_count`;
- `max_attempts`;
- `leased_by`;
- `lease_expires_at`;
- `heartbeat_at`;
- `run_after`;
- `priority`.

## Worker behavior

Worker execution flow:

1. Atomically claim one eligible queued job.
2. Set `status = running`, `leased_by`, `lease_expires_at`, `heartbeat_at`, and increment `attempt_count`.
3. If the job has `lock_key`, acquire the corresponding `job_lock`.
4. Execute the job handler.
5. Periodically heartbeat while the job is running.
6. Save `result_payload`.
7. Release business `job_lock`, if acquired.
8. Mark the job `succeeded` or `failed`.

If a business lock cannot be acquired, the worker must clear the lease, return the job to `queued`, and set `run_after` using a short backoff.

## Expired leases

Workers must recover expired leases:

- if `status = running` and `lease_expires_at` is in the past, the job is considered abandoned;
- if `attempt_count < max_attempts`, the job is returned to `queued` with `run_after` backoff;
- if attempts are exhausted, the job is marked `failed`.

## Storage-specific claim semantics

PostgreSQL should use row-level locking such as `FOR UPDATE SKIP LOCKED`.

SQLite should use a short write transaction and conditional update. The adapter must verify affected rows to ensure only one worker claimed the job.

## Consequences

- Stage 03 must implement claim, heartbeat, lease expiry recovery, retry/backoff, and graceful worker shutdown.
- `job_locks` remain required for repository and other conflict-prone domain operations.
- Worker logs and job results must include `worker_id` for diagnostics.
