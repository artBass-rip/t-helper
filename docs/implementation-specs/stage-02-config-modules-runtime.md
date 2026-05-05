# Stage 02: Config, Modules and Runtime Lifecycle

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
- unknown config keys and deprecated aliases are rejected with `validation_error`;
- only `scanning.global_scan` is accepted for global scan roots;
- storage profile slots `current` and `migration` are implemented fully;
- active database switches only through successful `thelper-ctl -migrate-db`;
- failed DB migration does not change the active `current` profile;
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

## Remaining MVP blockers

- нет Stage 02 blockers после ADR 0010 и Stage 00 initial module registry decision.

## Traceability

- Roadmap: Stage 02.
- Acceptance: `ACC-MVP-007`, `ACC-MVP-008`, `ACC-MVP-009`, `ACC-MVP-010`.
- API: `GET/PUT /api/config`, `GET /api/modules`, `POST /api/modules/reload`, `POST /api/modules/restart`.
- Data model: `config_entries`, `module_states`.
- ADR: `0004`, `0008`, `0009`, `0010`.

## Риски

- расхождение file import и runtime source of truth;
- неявное сохранение секретов в `config_entries`;
- module lifecycle станет in-memory only и потеряет observability.
