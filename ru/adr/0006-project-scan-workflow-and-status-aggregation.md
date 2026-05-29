# ADR 0006: Project scan workflow and status aggregation

## Status

Accepted.

## Decision

Project scans use a non-blocking parent-child job workflow.

`POST /api/project-scans` creates:

- one `project_scans` row;
- one parent `jobs.job_type = project_scan` job;
- a `job_group_id` with value `project_scan:<project_scan_id>`.

The parent `project_scan` job performs project-level checks and creates child `security_validation_scan` jobs when project security settings enable security modules. Parent jobs do not wait for child jobs.

Each job emits status events and metrics. A logical `status-monitor` module aggregates job, workflow, worker and module status into read models consumed by UI and internal services.

No `security_scans` persistent entity is introduced.

## Rationale

This keeps one public project scan endpoint while preserving module separation between `project-scanner` and `security-validator`.

Non-blocking parent orchestration avoids holding worker slots while child jobs run. A dedicated status aggregation path prevents UI and internal services from duplicating workflow status logic.

## Status aggregation rules

Workers write facts:

- `jobs.status`;
- `jobs.result_payload`;
- `job_events`;
- domain results such as `security_findings`.

`status-monitor` owns aggregates:

- `workflow_statuses`;
- aggregate `project_scans.status`;
- aggregate `project_scans.result_payload`.

UI and internal services must read status through aggregate read models and documented status/project scan endpoints.

## Project scan workflow

1. API creates `project_scans`.
2. API creates parent `project_scan` job.
3. Parent job runs project-level checks.
4. Parent job creates child `security_validation_scan` job(s), if required.
5. Parent job completes without waiting for child jobs.
6. Child jobs execute independently.
7. `status-monitor` aggregates all jobs with the same `job_group_id`.

## Consequences

- `jobs` needs `parent_job_id` and `job_group_id`.
- `job_events` and `workflow_statuses` are required read-model support entities.
- `GET /api/project-scans/{id}` returns aggregate status.
- Status endpoints expose workflow, job, worker and module status from the aggregation layer.
- Stage 03 implements the basic status-monitor read model for jobs/workers/modules.
- Stage 06 uses the status-monitor for project scan workflow aggregation.
