# Changelog

All notable repository changes are documented here.

This project does not have semantic version tags yet. Historical entries are
grouped by completed roadmap stages and merge commits.

## Unreleased

- Added bilingual GitHub Pages under `docs/index.html` and `docs/en.html`,
  including a visible AI-only implementation notice at the top of both pages.
- Added GitHub Pages deployment through `.github/workflows/pages.yml` from the
  `docs` directory.
- Configured the Pages workflow to request automatic Pages enablement before
  uploading the deployment artifact.
- Added GitHub Pages documentation and linked it from both README variants.
- Updated development, local environment and test-plan documentation to
  describe the current Stage 04 baseline.
- Next planned implementation stage: Stage 05 repository manager MVP.
- Stage 09 implementation spec and roadmap now capture the prioritized
  runtime/scanner optimization backlog from the code review.
- Russian changelog added as [`CHANGELOG.ru.md`](CHANGELOG.ru.md).
- English README promoted to the primary [`README.md`](README.md).
- Russian README added as [`README.ru.md`](README.ru.md).
- README content expanded with Stage 04 status, executable commands, HTTP API
  surface, configuration model, storage/migration scope, scanner behavior,
  documentation map, roadmap status and security/privacy assumptions.

## Stage 04: Scanner and Registry MVP - 2026-05-26

Commits: `490da8e` through `274dcee`.

### Added

- Added Stage 04 SQLite/PostgreSQL migrations for `root_paths`, `projects`,
  `project_links`, minimal generic `repositories`, `environments` and
  `workspaces`.
- Added scanner/registry API endpoints for root paths, scans, projects,
  project links, repositories, ignore rules, environments and workspaces.
- Added `global_scan` job handling that discovers Terraform working
  directories by `*.tf` filenames only.
- Added coalesced background `project_discovery` jobs for Git repository
  discovery after global scan registration.
- Added Git marker handling for `.git/` directories and `.git` files with
  `gitdir:` pointers.
- Added generic local repository cards and `same_repository` project links for
  separate projects discovered under the same repository.
- Added filesystem boundary tests and scanner filesystem abstraction.

### Changed

- Global scanner now skips symlinked directories and `.git/` directories,
  stops traversal below a discovered Terraform project and records skipped
  symlink/error counters.
- Root paths now track their source and inactive discovery paths are skipped.
- Ignore matching is exclude-only for Stage 04 and preserves `!pattern` rules
  without applying negation until full `.gitignore` semantics are implemented.
- Project listing defaults to active projects, with explicit filters for
  missing, disabled or all records.
- API documentation was aligned with mixed root scans, project links and Stage
  04 scanner behavior.

### Known Limits

- `POST /api/project-scans` exposes only the Stage 06 lifecycle guard and
  returns `project_scan_unavailable` for otherwise valid active projects.
- Repository clone, pull and sync workflows are reserved for Stage 05.
- Full `.gitignore` negation semantics are reserved for a later hardening
  stage.

## Stage 03: Jobs, Workers and Status Foundation - 2026-05-21

Merge commit: `7732215` (`stage-03-jobs-workers-status`).

### Added

- Added Stage 03 SQLite/PostgreSQL migrations for `jobs`, `job_locks`,
  `job_events` and `workflow_statuses`.
- Added persistent job enqueue, atomic claim, leases, heartbeat, retry/backoff
  and expired lease recovery.
- Added `thelper-worker` execution of queued background jobs.
- Added worker handlers for `config_reload` and `module_restart`.
- Added job idempotency scoped by actor, job type and `Idempotency-Key`.
- Added `/api/jobs`, `/api/jobs/{id}` and `/api/status*` read APIs.
- Added runtime, workflow, job and worker status read models.

### Changed

- Job idempotency compares canonical JSON payloads, so equivalent payloads with
  different object ordering or whitespace replay the same job.
- Worker status reports every running lease, including concurrent jobs owned by
  the same worker process.
- SQLite workers enforce the configured one-process limit with a
  database-fingerprint worker lock.
- Retention cleanup preserves events for active jobs and only deletes old
  routine events for terminal jobs.
- Job enqueue rejects secret-like payloads before persistence.

## Stage 02 Hardening and Documentation Alignment - 2026-05-06

Commit: `0b5996e` (`Stage02: preserve ignore_rules and sync reload`).

### Fixed

- Fixed `PUT /api/config` semantics so HTTP config imports preserve existing
  imported system `ignore_rules`; `.t-helper.ignore` remains owned by
  `thelper-ctl -reconfigure` and later ignore-rules APIs.
- Fixed `POST /api/modules/reload` request handling so malformed JSON returns
  `validation_error` instead of silently falling back to an all-key reload.
- Fixed `POST /api/modules/restart` validation so an empty `module_name`
  returns `validation_error`.
- Fixed config reload behavior so reloads that include `modules.enabled`
  re-seed persisted `module_states` from the active runtime config.
- Fixed `thelper-ctl -reload` to apply the same persisted module-state refresh
  path as the HTTP reload flow.

### Added

- Added tests for preserving imported `ignore_rules` during config-only import.
- Added HTTP tests for malformed module reload JSON and missing restart
  `module_name`.
- Added Stage 02 synchronous result schemas to payload documentation:
  `config_import.result.v1`, `config_reload.result.v1`,
  `module_reload.result.v1`, `module_restart.result.v1` and
  `storage_migration.result.v1`.

### Changed

- Aligned README, roadmap, development docs and local environment docs with the
  completed Stage 02 executable baseline.
- Clarified that Stage 02 config/module lifecycle endpoints are synchronous and
  do not create Stage 03 `jobs`.
- Clarified Stage 02 ownership of imported system `ignore_rules`, while Stage
  04 owns scanner/API behavior for root-path and project ignore rules.
- Updated API, traceability, data model, test plan and Stage 02 implementation
  spec to match the code-level Stage 02 contract.

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
- Added `thelper-ctl` commands: `reconfigure`, `reload`, `restart` and
  `migrate-db`.
- Added HTTP endpoints for config and module lifecycle operations.
- Added initial module registry seed for core MVP modules.
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
- `GET /api/config` masks sensitive values and does not expose resolved
  secrets.
- Storage target changes update the `migration` profile first; the active
  `current` profile changes only after successful `thelper-ctl migrate-db`.
- Module reload/restart operations now update persisted module state.
- `GET /api/health` keeps the Stage 01 `health_status.v1` response shape while
  adding singleton runtime lock/probe semantics.

### Fixed

- Storage profile imports keep active `current` storage stable until a
  successful `thelper-ctl -migrate-db`.
- Repeated storage migrations use fingerprint-specific migration profile IDs
  and retire previous migration targets before staging a new one.
- Re-importing config into an already promoted storage target preserves the
  active profile identity.
- `repositories.poll_interval_default` rejects zero and negative durations.
- Reload results distinguish accepted keys from actually applied keys.
- Module lifecycle failures persist `state = failed` with `last_error`.
- Runtime startup fails closed on unexpected `current` storage profile read
  errors instead of silently falling back to bootstrap storage.
- PostgreSQL storage profile DSNs URL-escape credentials and use
  `net.JoinHostPort` for IPv6-safe host formatting.
- Module reload/restart and `PUT /api/config` reject unknown fields, trailing
  payloads and invalid `null` requests where applicable.

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
- MySQL and MSSQL are accepted in forward-compatible configuration spelling,
  but runtime migration is currently implemented for SQLite and PostgreSQL
  only.

## Stage 01: Backend Storage Foundation - 2026-05-05

Merge commit: `eec0256` (`stage-01-backend-storage-foundation`).

### Added

- Added Go module `github.com/artBass-rip/t-helper`.
- Added executable entrypoints: `cmd/thelper`, `cmd/thelper-worker` and
  `cmd/thelper-ctl`.
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

- Current implemented backend scope is Stage 04.
- Current supported storage adapters are SQLite and PostgreSQL.
- Current public runtime API surface is health, config, module lifecycle,
  jobs/status and scanner/registry.
- Frontend directory exists as a placeholder; React/Tauri implementation starts
  in Stage 08.
