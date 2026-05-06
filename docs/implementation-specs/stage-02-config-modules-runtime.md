# Stage 02: Config, Modules and Runtime Lifecycle

## Status

Completed.

Stage 02 implements synchronous config/module lifecycle operations. It does not
create Stage 03 `jobs`; reload/restart responses use Stage 02 result DTOs and
persist runtime observability in `module_states`.

## Цель

Реализовать persisted runtime configuration, module lifecycle и local singleton runtime policy.

## Inputs

- `docs/configuration.md`
- `docs/data-model.md`
- `docs/interfaces.md`
- `docs/api.md`
- `docs/technology-stack.md`
- `docs/adr/0004-frontend-and-tauri-runtime-policy.md`
- `docs/adr/0008-configuration-key-compatibility.md`
- `docs/adr/0009-secret-resolution.md`
- `docs/adr/0010-singleton-runtime-lock-and-health.md`

## Scope

- `config_entries`;
- `module_states`;
- import через `thelper-ctl -reconfigure`;
- reload через `thelper-ctl -reload`;
- `thelper-ctl -restart <module>`;
- module registry с lifecycle `start`, `stop`, `reload`, `health`;
- initial module registry seed: `core`, `worker-runtime`, `config-manager`, `module-runtime`, `status-monitor`, `global-scanner`, `repository-manager`, `project-scanner`, `security-validator`, `auth`, `web`;
- singleton runtime lock/health mechanism;
- masking secrets в `GET /api/config`;
- policy для `secretref://...`.

## Non-goals

- long-running job execution;
- scanner/repository/security/auth handlers;
- frontend runtime startup implementation;
- distributed service discovery.

## Deliverables

- config validation/import pipeline;
- strict config schema validation with unknown key rejection;
- module registry;
- initial module registry seed;
- module state persistence;
- config/module API endpoints;
- CLI commands для reconfigure/reload/restart;
- full storage profile migration command `thelper-ctl -migrate-db`;
- singleton runtime discovery contract using the existing `health_status.v1` response schema.

## Definition of Done

- `thelper-ctl -reconfigure` атомарно импортирует config и ignore rules;
- `PUT /api/config` imports config atomically without clearing imported system
  `ignore_rules`, because `.t-helper.ignore` is not part of the HTTP payload;
- unknown config keys and deprecated aliases are rejected with `validation_error`;
- only `scanning.global_scan` is accepted for global scan roots;
- storage profile slots `current` and `migration` are implemented fully;
- active database switches only through successful `thelper-ctl -migrate-db`;
- failed DB migration does not change the active `current` profile;
- SQLite -> PostgreSQL `thelper-ctl -migrate-db` is covered end-to-end for
  Stage 02-owned tables using `secretref://env/...` credentials;
- runtime читает конфигурацию из БД, а не из файлов;
- reload возвращает applied keys и restart-required keys;
- module restart обновляет `module_states`;
- `modules.enabled` отклоняет unknown module names;
- registered but unavailable modules возвращаются в `GET /api/modules` со state `unavailable`;
- restart/reload для unavailable module возвращает controlled error;
- sensitive literal values are rejected;
- secrets не сохраняются и не возвращаются в открытом виде;
- `GET /api/config` returns masked sensitive values and never returns resolved secrets;
- Stage 02 extends the existing Stage 01 `GET /api/health` endpoint with singleton runtime lock/probe semantics and must not introduce a breaking `health_status.v1` response schema change;
- runtime lock metadata `config_database_fingerprint` matches the safe `database_fingerprint` returned by `/api/health`;
- повторный local runtime не создаёт второй активный процесс.

## Verification

Stage 02 baseline commands:

```text
go test -timeout 60s ./...
go test -race -timeout 90s ./...
go vet ./...
go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl
docker compose --profile offline -f docker-compose.test.yml run --rm test-runner
go test -cover ./...
```

Covered Stage 02 checks:

- strict config import rejects unknown top-level and nested keys;
- `scanning.global_scan` is the only accepted global scan roots key;
- sensitive literals for `external_databases.username/password` are rejected;
- `GET /api/config` masks secret refs and never resolves secrets;
- `thelper-ctl -reconfigure` imports config and ignore rules atomically;
- storage target changes update `migration` profile without changing active
  `current`;
- `thelper-ctl -migrate-db` switches active database only after successful
  target migration/copy;
- failed migration leaves active `current` unchanged;
- SQLite -> PostgreSQL migration is tested with `secretref://env/...`;
- runtime applies persisted `current` storage profile and runtime settings from
  DB on startup;
- module registry seeds all initial modules and persists `module_states`;
- `modules.enabled` controls available module running/stopped state;
- unavailable modules are returned as `unavailable`;
- module restart/reload uses lifecycle `stop/start/reload/health` hooks and
  returns controlled errors for unavailable modules;
- config reload validates request JSON and applies `modules.enabled` changes to
  persisted `module_states`;
- singleton lock writes `config_database_fingerprint` matching `/api/health`
  `database_fingerprint` and fails closed on ambiguous live PID/health state.

## Remaining MVP blockers

- нет Stage 02 blockers после ADR 0010 и Stage 00 initial module registry decision.

## Traceability

- Roadmap: Stage 02.
- Acceptance: `ACC-MVP-007`, `ACC-MVP-008`, `ACC-MVP-009`, `ACC-MVP-010`.
- API: `GET/PUT /api/config`, `GET /api/modules`, `POST /api/modules/reload`, `POST /api/modules/restart`.
- Data model: `config_entries`, `storage_profiles`,
  `storage_provider_settings`, `module_states`, imported system
  `ignore_rules`.
- ADR: `0004`, `0008`, `0009`, `0010`.

## Риски

- расхождение file import и runtime source of truth;
- неявное сохранение секретов в `config_entries`;
- module lifecycle станет in-memory only и потеряет observability.

## Notes for Later Stages

`thelper-ctl -migrate-db` copies Stage 02-owned tables:

- `system_metadata` and migration metadata through adapter migrations;
- `config_entries`;
- `storage_profiles`;
- `storage_provider_settings`;
- `module_states`;
- `ignore_rules`.

When later stages introduce their own persistent tables, they must extend the
storage migration copy/verification contract in the same stage that owns those
tables. Examples: Stage 03 owns `jobs`, `job_locks`, `job_events` and workflow
state; Stage 06 owns findings; Stage 07 owns auth/RBAC/audit.

`mysql` and `mssql` remain Stage 10 adapter-expansion targets. Stage 02 accepts
their config spelling for forward-compatible validation, but current MVP runtime
registry can migrate only between implemented adapters (`sqlite`, `postgres`).
