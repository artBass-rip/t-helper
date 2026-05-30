# Code Optimization And Quality Baseline

Last updated: 2026-05-31.

This document records completed optimizations, the current quality gate and the
remaining backlog, which should be implemented iteratively without broad
mechanical refactoring.

## Current Status

Completed:

- HTTP route composition was moved from `optionalHandlers ...any` and
  `type-switch` to the compile-time `httpapi.RouteRegistrar` contract.
- Route registration was moved closer to handlers through
  `RegisterRoutes(chi.Router)`.
- A smoke test was added for the full set of current HTTP routes:
  `internal/httpapi/router_test.go`.
- The transactional helper `storage.WithTx(ctx, db, opts, fn)` was added and
  applied in mutable lifecycle paths `jobs.Store.Start`,
  `jobs.Store.Complete`, `jobs.Store.Requeue`.
- Initial benchmarks were added for hot paths `jobs.Store.ClaimNext` and
  `jobs.Store.RefreshWorkflowStatus`.
- A local quality gate `make test` was added; it runs:
  `gofmt` check, `go vet ./...`, `go test ./...`.
- `make race` was added for manual/nightly checks with the race detector.

Verified baseline after optimization:

```text
make test
```

Note: some tests open a local listener on `127.0.0.1:0`, so sandboxed
environments may require bind permission.

Repository audit on 2026-05-31 found the executable baseline aligned with
Stage 05. The main documentation drift was in development/local-environment
docs that still described Stage 04 as current; those docs now reference the
Stage 05 repository manager APIs, migrations and worker behavior.

## Accepted Design Decisions

- Canonical decision for HTTP route registration and the transactional helper:
  [`adr/0019-code-quality-and-route-registration.md`](adr/0019-code-quality-and-route-registration.md).
- Stage 09 remains the owner of runtime/scanner performance hardening:
  [`implementation-specs/stage-09-runtime-observability-hardening.md`](implementation-specs/stage-09-runtime-observability-hardening.md).

## Backlog

### P1. SQL dialect helper

Problem: the store layer still contains branches by `handle.Provider` for
SQLite/PostgreSQL placeholders, casts and upsert shapes.

Next step:

- add a small helper in `internal/storage` or `internal/storage/sqlutil`;
- centralize placeholders, `IN (...)`, boolean/time/JSON casts and common
  upsert fragments;
- migrate store files one by one, starting with `jobs` or `scanner`.

### P1. Workflow status refresh load

Problem: `RefreshWorkflowStatus` is called after many job lifecycle events and
recomputes the aggregate read model.

Next step:

- use the added benchmark as the baseline;
- add a 10k-job scenario;
- after measurement, choose debounce, a dirty-workflow queue or a status-monitor
  reconciliation loop.

### P1. Split Large Store Files

Problem: `scanner/store.go`, `config/store.go`, `jobs/store.go` and
`repository/store.go` are still accumulation files.

Next step:

- split files by aggregate only when changing the corresponding area;
- keep the package-level API and tests without changing public behavior;
- start with `jobs` after introducing the SQL helper, so duplicate
  dialect code.

### P2. Filesystem scan backpressure

Problem: large root paths may create uneven load through frequent
progress/error events and per-item DB writes.

Next step:

- add a coarse-grained progress interval;
- evaluate batch upsert for projects;
- introduce scan metrics: directories/sec, DB writes/sec, skipped/error counts.

### P2. Repository operations separation

Problem: repository handlers combine validation, reservation lifecycle,
credential env, filesystem checks and `git` execution.

Next step:

- move `git` execution into `GitRunner`;
- move reservation lifecycle into a helper with cleanup;
- cover a fake `GitRunner` with tests for git/credential/reservation errors.

### P2. PostgreSQL connection pool settings

Problem: the SQLite provider explicitly limits the pool, while PostgreSQL
currently uses `database/sql` defaults.

Next step:

- add storage config for max open/idle connections, lifetime and idle time;
- set safe defaults for server/worker;
- log effective pool settings on startup.

### P3. Additional Benchmarks and Test Slicing

Next step:

- add benchmarks for `scanner.Store.ListProjects/ListRepositories`,
  scanner traversal and repository reservation conflict path;
- split large integration test files by endpoint/aggregate during the next
  changes in those areas;
- move shared fixtures into `*_test_helpers.go`.
