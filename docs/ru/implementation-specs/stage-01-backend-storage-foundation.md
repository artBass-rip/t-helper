# Stage 01: Backend Storage Foundation

## Status

Completed.

Implementation commits:

- `8dc572e` - initial scaffold: server, worker, storage.
- `e0f8f70` - CLI/server refactor, storage fixes and tests.

GitHub Actions checks passed for `ci / go` on both `push` and `pull_request`.

## Цель

Создать минимальный backend/runtime skeleton и storage foundation, на которые смогут опираться config, jobs и доменные модули.

## Inputs

- `docs/ru/adr/0001-go-backend-http-stack.md`
- `docs/ru/adr/0002-storage-and-migrations.md`
- `docs/ru/adr/0007-implementation-repository-layout-and-migrations.md`
- `docs/ru/development.md`
- `docs/ru/api.md`
- `docs/ru/data-model.md`
- `docs/ru/technology-stack.md`
- `docs/ru/configuration.md`

## Scope

- Go module и базовая структура пакетов;
- исполняемые entrypoints `thelper`, `thelper-worker`, `thelper-ctl`;
- HTTP skeleton на `net/http`/`chi`;
- storage abstraction;
- migrations framework;
- adapters для `SQLite` и `PostgreSQL`;
- pluggable storage provider registry для database adapters;
- stage-owned минимальные системные таблицы для backend skeleton;
- базовое логирование, correlation IDs и health endpoint.

## Non-goals

- config import/reload;
- module lifecycle;
- worker job execution;
- singleton runtime lock enforcement; Stage 01 returns the final safe health DTO shape, while Stage 02 adds lock/probe semantics;
- scanner/repository/security/auth доменная логика;
- frontend.

## Deliverables

- buildable Go skeleton - delivered;
- `cmd/thelper`, `cmd/thelper-worker`, `cmd/thelper-ctl` - delivered;
- storage interfaces - delivered;
- SQLite/PostgreSQL connection и migration bootstrap - delivered;
- storage provider registry with SQLite/PostgreSQL adapters - delivered;
- базовый HTTP server и `GET /api/health` endpoint returning final `health_status.v1` DTO - delivered;
- storage compatibility test skeleton - delivered and exercised for SQLite/PostgreSQL.

## Definition of Done

- `thelper`, `thelper-worker` и `thelper-ctl` собираются - verified by `go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl`;
- миграции применяются к SQLite и PostgreSQL - verified by storage contract tests and Docker test runner;
- Stage 01 migrations do not pre-create target tables owned by later stages - verified by storage contract tests;
- storage abstraction не протекает в HTTP handlers - implemented via app wiring and storage provider registry;
- SQLite/PostgreSQL реализованы как подключаемые storage adapter libraries за общим interface - delivered;
- unknown storage provider отклоняется controlled validation error - covered by tests;
- `GET /api/health` доступен и возвращает final `health_status.v1` DTO from `docs/ru/api.md` and ADR 0010, even though singleton lock enforcement is completed in Stage 02 - covered by handler and runtime smoke tests;
- Stage 01 health response includes `instance_id`, `mode`, `database_fingerprint`, `started_at`, `readiness` and `schema_version` - covered by tests;
- Stage 01 health endpoint is unauthenticated and safe: it must not expose config values, filesystem paths, DSNs, users, secrets or object-scoped details - implemented with safe `database_fingerprint`;
- storage tests проходят на обоих MVP adapters - verified by local tests and Docker `offline` profile with PostgreSQL.

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

- нет Stage 01 blockers после ADR 0007 и `docs/ru/development.md`;
- конкретный migration framework остаётся recommendation из ADR 0002 (`goose`) и может быть заменён только отдельным ADR.

## Traceability

- Roadmap: Stage 01.
- Acceptance: `ACC-MVP-013`.
- Data model: базовый migration framework для persistent entities.
- ADR: `0001`, `0002`, `0003`, `0007`, `0012`.

## Риски

- ранняя привязка доменной логики к конкретному SQL dialect - mitigated by storage registry and adapter packages;
- расхождение SQLite/PostgreSQL constraints - mitigated by shared contract tests;
- чрезмерный framework layer вокруг `net/http` - avoided by lightweight `chi` router and isolated HTTP adapter.

## Notes for Stage 02

- `thelper-worker` is intentionally a scaffold; persisted jobs and worker execution start in Stage 03.
- `thelper-ctl -reconfigure`, `-reload`, `-restart <module>` and `-migrate-db`
  remain Stage 02 deliverables.
- Singleton runtime lock enforcement remains Stage 02; Stage 01 already provides
  the stable safe `health_status.v1` response shape.
- Stage 01 migrations own only `system_metadata`; Stage 02 must add
  `config_entries`, `storage_profiles`, `storage_provider_settings` and
  `module_states` through new append-only migrations.
