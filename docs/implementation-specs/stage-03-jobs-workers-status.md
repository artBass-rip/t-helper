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
- worker handlers для `config_reload` и `module_restart`;
- basic status-monitor для jobs/workers/modules;
- status API endpoints.

## Non-goals

- filesystem scanning;
- repository operations;
- project/security scan toolchain;
- auth/RBAC enforcement beyond system contracts.

## Deliverables

- `thelper-worker` execution loop;
- job lease implementation для SQLite/PostgreSQL;
- lock acquire/release semantics;
- status-monitor read models;
- job/status API;
- worker integration tests.

## Definition of Done

- API/CLI создают jobs в `queued`, но не выполняют long-running work inline;
- только один worker может claim один job;
- heartbeat обновляется для running jobs;
- expired leases восстанавливаются через retry/backoff или failure после `max_attempts`;
- `job_locks` не допускают конфликтующие active operations;
- status endpoints читают aggregate read models.

## Remaining MVP blockers

- нет Stage 03 blockers после ADR 0011.

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
