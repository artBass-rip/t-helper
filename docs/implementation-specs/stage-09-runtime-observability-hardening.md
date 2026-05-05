# Stage 09: Runtime and Observability Hardening

## Цель

Довести runtime, scheduler, workers, module lifecycle и status-monitor до эксплуатационно устойчивого состояния, пригодного для дальнейших platform stages и distributed deployment.

## Inputs

- `docs/roadmap.md`
- `docs/test-plan.md`
- `docs/data-model.md`
- `docs/access-control.md`
- `docs/technology-stack.md`
- `docs/payload-schemas.md`

## Scope

- runtime/module hardening;
- scheduler hardening;
- expanded observability/status-monitor;
- full `.gitignore` semantics with `!pattern`;
- optional `follow_symlinks = true` hardening with cycle detection, root containment checks and traversal guards, если этот режим утверждён;
- worker graceful shutdown/recovery;
- retry, timeout and retention hardening;
- operator diagnostics for jobs, locks, modules, workers and scans.

## Non-goals

- multi-node orchestration;
- additional storage adapters;
- additional security adapters or policy packs;
- administrative UI;
- full SCIM sync workflow;
- webhook-based repository sync;
- incompatible data model changes;
- second source of truth for configuration.

## Deliverables

- observability extensions;
- hardened scheduler/runtime;
- full `.gitignore` matcher;
- optional hardened symlink traversal if approved;
- worker shutdown/recovery and retention tests;
- operator diagnostics and degraded-state status payloads.

## Definition of Done

- observability covers jobs, locks, modules, workers and scans;
- expired leases, stuck locks, degraded modules and failed scans are visible through status/diagnostic endpoints;
- scheduler and worker shutdown/recovery behavior is deterministic and tested;
- full `.gitignore` semantics including `!pattern` passes acceptance tests;
- optional `follow_symlinks = true`, if accepted, includes cycle detection, root containment checks and traversal guards;
- retention cleanup for job events and released/expired locks is deterministic and does not remove active jobs/locks or audit records.

## Platform blockers

- observability backend/export format.

## Traceability

- Roadmap: Stage 09.
- Acceptance: `ACC-PLATFORM-004` and operational prerequisites for `ACC-PLATFORM-005`.
- Platform criteria are defined in `docs/roadmap.md`.

## Риски

- hardening expands into feature work from later stages;
- observability model grows without clear operator workflows.
