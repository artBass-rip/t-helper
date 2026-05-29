# Stage 02: Config, Modules and Runtime Lifecycle

## Status

Completed.

Stage 02 implements synchronous config/module lifecycle operations. It does not
create Stage 03 `jobs`; reload/restart responses use Stage 02 result DTOs and
persist runtime observability in `module_states`.

## Goal

Implement persisted runtime configuration, module lifecycle and local singleton runtime policy.

## Inputs

- `docs/en/configuration.md`
- `docs/en/data-model.md`
- `docs/en/interfaces.md`
- `docs/en/api.md`
- `docs/en/technology-stack.md`
- `docs/en/adr/0004-frontend-and-tauri-runtime-policy.md`
- `docs/en/adr/0008-configuration-key-compatibility.md`
- `docs/en/adr/0009-secret-resolution.md`
- `docs/en/adr/0010-singleton-runtime-lock-and-health.md`

## Scope

- `config_entries`;
- `module_states`;
- import through `thelper-ctl -reconfigure`;
- reload through `thelper-ctl -reload`;
- `thelper-ctl -restart <module>`;
- module registry with lifecycle `start`, `stop`, `reload`, `health`;
- initial module registry seed: `core`, `worker-runtime`, `config-manager`, `module-runtime`, `status-monitor`, `global-scanner`, `repository-manager`, `project-scanner`, `security-validator`, `auth`, `web`;
- singleton runtime lock/health mechanism;
- masking secrets in `GET /api/config`;
- policy for `secretref://...`.

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
- CLI commands for reconfigure/reload/restart;
- full storage profile migration command `thelper-ctl -migrate-db`;
- singleton runtime discovery contract using the existing `health_status.v1` response schema.

## Definition of Done

- `thelper-ctl -reconfigure` atomically imports config and ignore rules;
- `PUT /api/config` imports config atomically without clearing imported system
  `ignore_rules`, because `.t-helper.ignore` is not part of the HTTP payload;
- unknown config keys and deprecated aliases are rejected with `validation_error`;
- config/module HTTP JSON payloads reject malformed JSON, unknown fields where
  strict request structs are used, `null` object payloads and trailing payload;
- only `scanning.global_scan` is accepted for global scan roots;
- storage profile slots `current` and `migration` are implemented fully;
- active database switches only through successful `thelper-ctl -migrate-db`;
- initial import with `external_databases.enabled = true` stages the external
  database as `migration` target while keeping `database` as active `current`;
- later storage target imports after a successful migration do not overwrite the
  active `current` profile before the next successful `thelper-ctl -migrate-db`;
- failed DB migration does not change the active `current` profile;
- SQLite -> PostgreSQL `thelper-ctl -migrate-db` is covered end-to-end for
  Stage 02-owned tables using `secretref://env/...` credentials;
- runtime reads configuration from the DB, not from files;
- reload returns accepted keys, actually applied Stage 02 keys and
  restart-required keys; accepted-but-not-applied reloadable keys must not be
  misreported as applied;
- module restart updates `module_states`;
- `modules.enabled` rejects unknown module names;
- registered but unavailable modules are returned in `GET /api/modules` with state `unavailable`;
- restart/reload for an unavailable module returns a controlled error;
- sensitive literal values are rejected;
- secrets are not stored or returned in cleartext;
- `GET /api/config` returns masked sensitive values and never returns resolved secrets;
- Stage 02 extends the existing Stage 01 `GET /api/health` endpoint with singleton runtime lock/probe semantics and must not introduce a breaking `health_status.v1` response schema change;
- runtime lock metadata `config_database_fingerprint` matches the safe `database_fingerprint` returned by `/api/health`;
- a repeated local runtime does not create a second active process.

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
- initial external database import on an empty bootstrap store keeps SQLite as
  `current` and stages PostgreSQL as `migration`;
- repeated SQLite target migrations after a successful promotion keep active
  `current` stable until the next successful `migrate-db`;
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
- explicit unknown reload keys are returned in `failed_keys` and are not
  reported as accepted or applied;
- module lifecycle failures persist `state = failed` with `last_error` in
  `module_states.details`;
- PostgreSQL storage profile DSNs are built with URL-escaped credentials and
  `net.JoinHostPort` host formatting;
- runtime startup fails closed on unexpected `current` storage profile read
  errors and only falls back to bootstrap storage when no current profile exists;
- singleton lock writes `config_database_fingerprint` matching `/api/health`
  `database_fingerprint` and fails closed on ambiguous live PID/health state.

## Remaining MVP blockers

- no Stage 02 blockers remain after ADR 0010 and the Stage 00 initial module registry decision.

## Traceability

- Roadmap: Stage 02.
- Acceptance: `ACC-MVP-007`, `ACC-MVP-008`, `ACC-MVP-009`, `ACC-MVP-010`.
- API: `GET/PUT /api/config`, `GET /api/modules`, `POST /api/modules/reload`, `POST /api/modules/restart`.
- Data model: `config_entries`, `storage_profiles`,
  `storage_provider_settings`, `module_states`, imported system
  `ignore_rules`.
- ADR: `0004`, `0008`, `0009`, `0010`.

## Risks

- divergence between file import and the runtime source of truth;
- implicit storage of secrets in `config_entries`;
- module lifecycle becomes in-memory only and loses observability.

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
