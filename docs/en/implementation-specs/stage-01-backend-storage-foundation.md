# Stage 01: Backend Storage Foundation

## Status

Completed.

Implementation commits:

- `8dc572e` - initial scaffold: server, worker, storage.
- `e0f8f70` - CLI/server refactor, storage fixes and tests.

GitHub Actions checks passed for `ci / go` on both `push` and `pull_request`.

## Goal

Create a minimal backend/runtime skeleton and storage foundation that config, jobs and domain modules can rely on.

## Inputs

- `docs/en/adr/0001-go-backend-http-stack.md`
- `docs/en/adr/0002-storage-and-migrations.md`
- `docs/en/adr/0007-implementation-repository-layout-and-migrations.md`
- `docs/en/development.md`
- `docs/en/api.md`
- `docs/en/data-model.md`
- `docs/en/technology-stack.md`
- `docs/en/configuration.md`

## Scope

- Go module and base package structure;
- executable entrypoints `thelper`, `thelper-worker`, `thelper-ctl`;
- HTTP skeleton on `net/http`/`chi`;
- storage abstraction;
- migrations framework;
- adapters for `SQLite` and `PostgreSQL`;
- pluggable storage provider registry for database adapters;
- stage-owned minimal system tables for the backend skeleton;
- basic logging, correlation IDs and health endpoint.

## Non-goals

- config import/reload;
- module lifecycle;
- worker job execution;
- singleton runtime lock enforcement; Stage 01 returns the final safe health DTO shape, while Stage 02 adds lock/probe semantics;
- scanner/repository/security/auth domain logic;
- frontend.

## Deliverables

- buildable Go skeleton - delivered;
- `cmd/thelper`, `cmd/thelper-worker`, `cmd/thelper-ctl` - delivered;
- storage interfaces - delivered;
- SQLite/PostgreSQL connection and migration bootstrap - delivered;
- storage provider registry with SQLite/PostgreSQL adapters - delivered;
- basic HTTP server and `GET /api/health` endpoint returning final `health_status.v1` DTO - delivered;
- storage compatibility test skeleton - delivered and exercised for SQLite/PostgreSQL.

## Definition of Done

- `thelper`, `thelper-worker` and `thelper-ctl` build - verified by `go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl`;
- migrations apply to SQLite and PostgreSQL - verified by storage contract tests and Docker test runner;
- Stage 01 migrations do not pre-create target tables owned by later stages - verified by storage contract tests;
- storage abstraction does not leak into HTTP handlers - implemented via app wiring and storage provider registry;
- SQLite/PostgreSQL are implemented as pluggable storage adapter libraries behind a shared interface - delivered;
- unknown storage provider is rejected with a controlled validation error - covered by tests;
- `GET /api/health` is available and returns the final `health_status.v1` DTO from `docs/en/api.md` and ADR 0010, even though singleton lock enforcement is completed in Stage 02 - covered by handler and runtime smoke tests;
- Stage 01 health response includes `instance_id`, `mode`, `database_fingerprint`, `started_at`, `readiness` and `schema_version` - covered by tests;
- Stage 01 health endpoint is unauthenticated and safe: it must not expose config values, filesystem paths, DSNs, users, secrets or object-scoped details - implemented with safe `database_fingerprint`;
- storage tests pass on both MVP adapters - verified by local tests and Docker `offline` profile with PostgreSQL.

## Verification

Stage 01 baseline commands:

```text
go test ./...
go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl
docker compose --profile offline -f docker-compose.test.yml run --rm test-runner
```

GitHub Actions:

- `ci / go (push)` - passed.
- `ci / go (pull_request)` - passed.

Covered Stage 01 checks:

- runtime smoke test for `GET /api/health`;
- handler-level `health_status.v1` shape test;
- CLI smoke test for `thelper-ctl providers`;
- controlled unknown provider and unknown CLI command errors;
- `postgresql` provider alias normalization to internal `postgres`;
- synchronized migration version test for `sqlite` and `postgres`;
- idempotent migration runner test;
- SQLite storage contract test in every local run;
- PostgreSQL storage contract test when `THELPER_POSTGRES_DSN` is set;
- destructive PostgreSQL storage test guard requiring a test database name or explicit override.

## Remaining MVP blockers

- no Stage 01 blockers remain after ADR 0007 and `docs/en/development.md`;
- the specific migration framework remains the recommendation from ADR 0002 (`goose`) and can be replaced only by a separate ADR.

## Traceability

- Roadmap: Stage 01.
- Acceptance: `ACC-MVP-013`.
- Data model: basic migration framework for persistent entities.
- ADR: `0001`, `0002`, `0003`, `0007`, `0012`.

## Risks

- early binding of domain logic to a specific SQL dialect - mitigated by storage registry and adapter packages;
- divergence between SQLite/PostgreSQL constraints - mitigated by shared contract tests;
- excessive framework layer around `net/http` - avoided by lightweight `chi` router and isolated HTTP adapter.

## Notes for Stage 02

- `thelper-worker` is intentionally a scaffold; persisted jobs and worker execution start in Stage 03.
- `thelper-ctl -reconfigure`, `-reload`, `-restart <module>` and `-migrate-db`
  remain Stage 02 deliverables.
- Singleton runtime lock enforcement remains Stage 02; Stage 01 already provides
  the stable safe `health_status.v1` response shape.
- Stage 01 migrations own only `system_metadata`; Stage 02 must add
  `config_entries`, `storage_profiles`, `storage_provider_settings` and
  `module_states` through new append-only migrations.
