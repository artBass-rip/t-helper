# Stage 01: Backend Storage Foundation

## Цель

Создать минимальный backend/runtime skeleton и storage foundation, на которые смогут опираться config, jobs и доменные модули.

## Inputs

- `docs/adr/0001-go-backend-http-stack.md`
- `docs/adr/0002-storage-and-migrations.md`
- `docs/adr/0007-implementation-repository-layout-and-migrations.md`
- `docs/development.md`
- `docs/api.md`
- `docs/data-model.md`
- `docs/technology-stack.md`
- `docs/configuration.md`

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

- buildable Go skeleton;
- `cmd/thelper`, `cmd/thelper-worker`, `cmd/thelper-ctl`;
- storage interfaces;
- SQLite/PostgreSQL connection и migration bootstrap;
- storage provider registry with SQLite/PostgreSQL adapters;
- базовый HTTP server и `GET /api/health` endpoint returning final `health_status.v1` DTO;
- storage compatibility test skeleton.

## Definition of Done

- `thelper`, `thelper-worker` и `thelper-ctl` собираются;
- миграции применяются к SQLite и PostgreSQL;
- Stage 01 migrations do not pre-create target tables owned by later stages;
- storage abstraction не протекает в HTTP handlers;
- SQLite/PostgreSQL реализованы как подключаемые storage adapter libraries за общим interface;
- unknown storage provider отклоняется controlled validation error;
- `GET /api/health` доступен и возвращает final `health_status.v1` DTO from `docs/api.md` and ADR 0010, even though singleton lock enforcement is completed in Stage 02;
- Stage 01 health response includes `instance_id`, `mode`, `database_fingerprint`, `started_at`, `readiness` and `schema_version`;
- Stage 01 health endpoint is unauthenticated and safe: it must not expose config values, filesystem paths, DSNs, users, secrets or object-scoped details;
- storage tests проходят на обоих MVP adapters.

## Remaining MVP blockers

- нет Stage 01 blockers после ADR 0007 и `docs/development.md`;
- конкретный migration framework остаётся recommendation из ADR 0002 (`goose`) и может быть заменён только отдельным ADR.

## Traceability

- Roadmap: Stage 01.
- Acceptance: `ACC-MVP-013`.
- Data model: базовый migration framework для persistent entities.
- ADR: `0001`, `0002`, `0003`, `0007`, `0012`.

## Риски

- ранняя привязка доменной логики к конкретному SQL dialect;
- расхождение SQLite/PostgreSQL constraints;
- чрезмерный framework layer вокруг `net/http`.
