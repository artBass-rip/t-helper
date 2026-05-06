# Changelog

All notable repository changes are documented here.

This project does not have semantic version tags yet. Historical entries are
grouped by completed roadmap stages and merge commits.

## Unreleased

- Stage 02 hardening: `PUT /api/config` now preserves imported system
  `ignore_rules` because `.t-helper.ignore` is not part of the HTTP config
  payload.
- Stage 02 hardening: `POST /api/modules/reload` now rejects malformed JSON
  with `validation_error` instead of silently reloading all keys.
- Stage 02 hardening: config reload applies `modules.enabled` changes to
  persisted `module_states`.
- Documentation was aligned with the completed Stage 02 baseline, including
  synchronous config/module result DTOs and Stage 02 ownership of system
  `ignore_rules`.
- Next planned implementation stage: Stage 03 jobs, workers and status
  foundation.

## Stage 02: Config, Modules and Runtime Lifecycle - 2026-05-06

Merge commit: `39446dd` (`stage-02-config-modules-runtime`).

### Added

- Added Stage 02 storage migrations for SQLite and PostgreSQL.
- Added persisted runtime configuration through `config_entries`.
- Added storage profile persistence through `storage_profiles` and
  `storage_provider_settings`.
- Added persisted module observability through `module_states`.
- Added `ignore_rules` import support as part of reconfiguration.
- Added strict configuration import and validation pipeline.
- Added `thelper-ctl` commands:
  - `reconfigure`
  - `reload`
  - `restart`
  - `migrate-db`
- Added HTTP endpoints:
  - `GET /api/config`
  - `PUT /api/config`
  - `GET /api/modules`
  - `POST /api/modules/reload`
  - `POST /api/modules/restart`
- Added initial module registry seed:
  - `core`
  - `worker-runtime`
  - `config-manager`
  - `module-runtime`
  - `status-monitor`
  - `global-scanner`
  - `repository-manager`
  - `project-scanner`
  - `security-validator`
  - `auth`
  - `web`
- Added singleton runtime lock and health metadata handling.
- Added Stage 02 end-to-end tests for config import, reload, module lifecycle,
  runtime startup, storage profile migration and singleton runtime behavior.

### Changed

- Runtime startup now reads active storage/runtime configuration from the
  database instead of treating file configuration as the source of truth.
- Configuration validation now rejects unknown top-level and nested keys.
- Deprecated configuration aliases are rejected instead of being accepted as
  alternate spellings.
- Global scan roots are accepted only through `scanning.global_scan`.
- `GET /api/config` masks sensitive values and does not expose resolved secrets.
- Storage target changes update the `migration` profile first; the active
  `current` profile changes only after successful `thelper-ctl migrate-db`.
- Module reload/restart operations now update persisted module state.
- `GET /api/health` keeps the Stage 01 `health_status.v1` response shape while
  adding singleton runtime lock/probe semantics.

### Security

- Sensitive literal values for external database credentials are rejected.
- Secret references are preserved as references and are not persisted or
  returned as plaintext secrets.
- Runtime lock metadata uses safe database fingerprints rather than raw DSNs or
  filesystem paths.

### Known Limits

- Stage 02 uses synchronous lifecycle operations and does not create Stage 03
  jobs.
- Scanner, repository manager, security validator, auth and frontend behavior
  remain future-stage deliverables.
- MySQL and MSSQL are accepted in forward-compatible configuration spelling, but
  runtime migration is currently implemented for SQLite and PostgreSQL only.

## Stage 01: Backend Storage Foundation - 2026-05-05

Merge commit: `eec0256` (`stage-01-backend-storage-foundation`).

### Added

- Added Go module `github.com/artBass-rip/t-helper`.
- Added executable entrypoints:
  - `cmd/thelper`
  - `cmd/thelper-worker`
  - `cmd/thelper-ctl`
- Added backend HTTP runtime scaffold using `net/http` and `chi`.
- Added unauthenticated safe `GET /api/health` endpoint returning
  `health_status.v1`.
- Added correlation ID middleware.
- Added storage abstraction and provider registry.
- Added SQLite and PostgreSQL storage providers.
- Added `postgresql` external provider name normalization to internal
  `postgres`.
- Added dialect-specific migration runner using `goose`.
- Added Stage 01 SQLite/PostgreSQL migrations for `system_metadata`.
- Added storage fingerprinting for safe health metadata.
- Added `thelper-ctl providers` diagnostics.
- Added buildable `thelper-worker` scaffold.
- Added GitHub Actions CI for formatting, tests and entrypoint builds.
- Added tests for storage registry behavior, migrations, storage contracts,
  health response shape and server startup.

### Changed

- Repository moved from documentation-only planning into executable backend
  scaffold.
- Stage-owned migration policy was enforced: Stage 01 creates only Stage
  01-owned system tables and does not pre-create later-stage product tables.
- README, development docs, roadmap, test plan and implementation specs were
  updated to reflect the Stage 01 executable baseline.

### Known Limits

- Worker job execution is scaffold-only and starts in Stage 03.
- Runtime configuration, module lifecycle and singleton lock enforcement are
  Stage 02 responsibilities.
- Product/domain behavior for scanner, repository manager, auth and frontend is
  reserved for later roadmap stages.

## Stage 00: Delivery Contract - 2026-05-05

Merge commit: `cf9474d` (`stage-00-delivery-contract`).

### Added

- Added Stage 00 delivery contract and decision register.
- Added repository-level implementation rules, Stage 01 scaffolding checklist
  and Stage 01-03 backlog.
- Added Docker Compose files for development, manual checks, OS matrix checks
  and test execution.
- Added `.artifacts` directory structure placeholders for local environment,
  logs, repositories, SQLite data and test outputs.
- Added local environment example file.
- Added implementation-spec documentation index updates.
- Added `.gitignore` entries for generated artifacts and local runtime files.

### Changed

- Clarified that Stage 00 is documentation and decision delivery only.
- Deferred executable scaffolding, CI workflow files and storage test harnesses
  to Stage 01.

## Initial Documentation Baseline - 2026-04-09 to 2026-05-05

### Added

- Added initial project README and configuration example.
- Added core product documentation for requirements, architecture, interfaces,
  API, configuration, data model, payload schemas, access control,
  traceability, test plan, roadmap and technology stack.
- Added ADRs covering backend stack, storage and migrations, job workers,
  frontend/Tauri runtime policy, configuration compatibility, secret
  resolution, singleton runtime health, repository identity/provider parsing,
  local password hashing, security finding fingerprints and external toolchain
  profiles.
- Added implementation specs for planned stages.

### Repository State

- Current implemented backend scope is Stage 02.
- Current supported storage adapters are SQLite and PostgreSQL.
- Current public runtime API surface is health, config and module lifecycle.
- Frontend directory exists as a placeholder; React/Tauri implementation starts
  in Stage 08.
