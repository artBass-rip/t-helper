# ADR 0003: Job worker processes

## Status

Accepted.

## Decision

Background jobs are executed by separate `thelper-worker` processes.

`thelper` creates jobs, validates requests, exposes API/runtime endpoints and manages configuration/module state. It does not execute long-running jobs inline.

## Rationale

- Separating API/runtime from job execution improves fault isolation.
- Worker processes prepare the system for distributed deployment.
- Persistent jobs and `job_locks` remain the shared coordination mechanism.

## Consequences

- Stage 01 must scaffold `thelper-worker`.
- Job handlers run in worker processes.
- Multiple workers may run concurrently when lock keys do not conflict.
- `job_locks` must be enforced across processes through persistent storage.
