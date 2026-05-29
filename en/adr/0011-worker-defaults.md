# ADR 0011: Worker defaults

## Status

Accepted.

## Decision

Workers use a stable diagnostic identity and bounded exponential retry defaults.

Worker identity format:

```text
<hostname>:<pid>:<worker_uuid>
```

`worker_uuid` is generated at worker process startup and remains stable for the lifetime of that process.

Default retry policy:

- `max_attempts`: `3`
- initial backoff: `5s`
- backoff multiplier: `2`
- max backoff: `5m`
- jitter: enabled, up to 20 percent

The retry delay is stored in `jobs.run_after`. `attempt_count` is incremented only after a successful atomic claim. A job that exhausts `max_attempts` is marked `failed`.

Business lock contention policy:

- if a worker cannot acquire `job_locks.lock_key`, it clears the job lease, returns the job to `queued`, and sets `run_after` using the retry policy;
- lock contention must not execute the job handler;
- lock contention emits a `job_events` entry with a machine-readable reason.

Retention defaults:

- `job_events`: 30 days for routine event payloads;
- released or expired `job_locks`: 30 days;
- completed `jobs`: retained until an explicit cleanup policy for jobs is introduced;
- `audit_log`: not covered by worker retention and must not be deleted by worker cleanup.

Retention cleanup must be explicit and observable. It must not delete events, locks or jobs that are still active.

## Rationale

Stage 03 needs deterministic worker behavior before implementation. Without shared defaults, handlers may diverge in retry timing, lock contention handling and diagnostic metadata.

Keeping completed jobs while expiring high-volume events and old locks preserves operational history without unbounded growth of the noisiest tables.

## Consequences

- Stage 03 uses these defaults unless runtime configuration overrides them.
- Worker logs, job events and failed job result payloads include `worker_id`.
- Tests must cover worker identity format, retry/backoff calculation, lock contention requeue behavior and retention safety.
