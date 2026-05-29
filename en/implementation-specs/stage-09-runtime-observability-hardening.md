# Stage 09: Runtime and Observability Hardening

## Goal

Bring the runtime, scheduler, workers, module lifecycle and status-monitor to an operationally resilient state suitable for later platform stages and distributed deployment.

## Inputs

- `docs/en/roadmap.md`
- `docs/en/test-plan.md`
- `docs/en/data-model.md`
- `docs/en/access-control.md`
- `docs/en/technology-stack.md`
- `docs/en/payload-schemas.md`

## Scope

- runtime/module hardening;
- scheduler hardening;
- expanded observability/status-monitor;
- runtime and scanner performance optimization backlog;
- full `.gitignore` semantics with `!pattern`;
- optional `follow_symlinks = true` hardening with cycle detection, root containment checks and traversal guards, if this mode is approved;
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
- prioritized runtime/scanner optimization changes with before/after tests or benchmarks;
- full `.gitignore` matcher;
- optional hardened symlink traversal if approved;
- worker shutdown/recovery and retention tests;
- operator diagnostics and degraded-state status payloads.

## Optimization backlog

Stage 09 owns optimization work that hardens already delivered Stage 03-05
runtime behavior without changing public API contracts. The current optimization
register and already completed quality work are tracked in
[`../code-optimization.md`](../code-optimization.md).

Completed before Stage 09:

- route registration now uses `httpapi.RouteRegistrar`;
- `storage.WithTx` exists for new or materially changed transaction paths;
- `make test` is the local quality gate;
- initial `jobs` benchmarks cover `ClaimNext` and
  `RefreshWorkflowStatus`.

Recommended implementation order:

1. Reduce `workflow_statuses` refresh amplification.

   Current job lifecycle paths refresh workflow status after enqueue, claim,
   start, progress events, completion and requeue. `RefreshWorkflowStatus`
   recalculates the workflow read model from `jobs` and `job_events` each time.
   Keep refreshes on durable state transitions, but avoid recalculating on every
   progress event. Acceptable approaches include a dirty-workflow queue,
   debounced refresh, or a status-monitor owned reconciliation loop.

2. Batch root path lookup by IDs.

   `scanner.Store.RootPathsByIDs` should resolve requested IDs through one
   `SELECT ... WHERE id IN (...)` query, deduplicate inputs, preserve requested
   order and still reject disabled roots. This removes N+1 reads from manual
   scan creation and `global_scan` payload handling.

3. Batch project persistence during global scans.

   `global-scanner` currently persists each discovered Terraform directory with
   an individual `UpsertProject` transaction and then enqueues discovery work.
   Add a batch path per root path: collect discovered project identities,
   upsert them in one transaction, return created/updated counters and enqueue
   `project_discovery` jobs after persistence succeeds. SQLite and PostgreSQL
   must keep equivalent conflict behavior.

4. Optimize worker status aggregation.

   `WorkerStatusesPage` should avoid a correlated `count(*)` per worker row and
   a broad anti-join when listing active/stale workers. Use a grouped/CTE shape
   for SQLite and a dialect-appropriate PostgreSQL query such as window
   functions or `DISTINCT ON`, then verify the existing worker status contract.

5. Align indexes with cursor queries.

   Review scanner and status list endpoints against their actual filters and
   `ORDER BY` clauses. Add or adjust composite indexes for common cursor queries
   such as `(status, created_at DESC, id DESC)` or switch endpoints to existing
   indexed ordering only when the documented API contract allows it.

6. Expand shared SQL dialect helpers.

   The `jobs`, `scanner` and `config` stores still duplicate placeholder, bool,
   time casting and upsert differences between SQLite and PostgreSQL. Introduce
   a small internal helper layer before Stage 10 adapter expansion so
   MySQL/MSSQL support does not multiply branch logic inside domain stores.

## Definition of Done

- observability covers jobs, locks, modules, workers and scans;
- expired leases, stuck locks, degraded modules and failed scans are visible through status/diagnostic endpoints;
- scheduler and worker shutdown/recovery behavior is deterministic and tested;
- optimization backlog items accepted for Stage 09 have regression tests and,
  where applicable, before/after query or benchmark evidence;
- full `.gitignore` semantics including `!pattern` passes acceptance tests;
- optional `follow_symlinks = true`, if accepted, includes cycle detection, root containment checks and traversal guards;
- retention cleanup for job events and released/expired locks is deterministic and does not remove active jobs/locks or audit records.

## Platform blockers

- observability backend/export format.

## Traceability

- Roadmap: Stage 09.
- Acceptance: `ACC-PLATFORM-004` and operational prerequisites for `ACC-PLATFORM-005`.
- Platform criteria are defined in `docs/en/roadmap.md`.

## Risks

- hardening expands into feature work from later stages;
- observability model grows without clear operator workflows.
