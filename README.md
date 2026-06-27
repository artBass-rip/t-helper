# T-Helper

[Русская версия](README.ru.md)

`t-helper` is an on-premise platform for discovering Terraform projects,
tracking repository metadata, running local security analysis, and managing
runtime configuration, modules, jobs and access control from one backend.

**Stage 05: Repository Manager MVP is closed.** The executable backend
foundation is implemented: service entrypoints, storage
adapters, migrations, health checks, persisted runtime configuration, module
lifecycle, singleton runtime locking, jobs/workers/status, global scanning,
Terraform project discovery, root paths, project registry, scanner/registry
HTTP APIs and repository manager clone/pull/sync workflows.

The next active roadmap block is Stage 06A/06B: external toolchain profiles,
project scanner and security validator MVP. Authentication/RBAC and the
frontend remain intentionally owned by later roadmap stages.

## Project Pages

GitHub Pages are provided as a static bilingual project landing page with
generated bilingual documentation pages:

- Russian page: [docs/index.html](docs/index.html)
- English page: [docs/en.html](docs/en.html)
- Deployment workflow: [.github/workflows/pages.yml](.github/workflows/pages.yml)

The landing page uses the current dark framed Pages layout: sticky navigation,
a product overview hero, the T-Helper mark, feature strip and dark
documentation shell. It explicitly states at the top that the project is
implemented exclusively by AI. Russian Markdown sources live under `docs/ru/`;
corresponding English Markdown sources live under `docs/en/` with the same
relative paths. During publication, `docs/build-pages.js` renders those paired
sources into Russian and English HTML shells with local navigation, related
documents and same-language links. Pages are published from the `docs`
directory through GitHub Actions to the `gh-pages` branch.
Repository Pages settings must use source `Deploy from a branch`, branch
`gh-pages`, folder `/ (root)`.

## License

Internal proprietary project. All rights reserved.

Use, copying, modification, distribution, and access outside the authorized
organization or team are prohibited without prior written permission from the
copyright holder.

## What Is Implemented

- Terraform working directory discovery by `*.tf` filenames.
- Persistent root path, project, project link, minimal repository,
  environment and workspace registries.
- Runtime configuration stored in the database and imported through
  `thelper-ctl`.
- Module state persistence, reload and restart operations.
- Background job queue with persistent leases, heartbeat, retry/backoff,
  `job_locks`, job events and status read models.
- `global_scan` jobs that enqueue coalesced `project_discovery` jobs for Git
  repository association without blocking the global scan result.
- Stage 05 repository manager APIs for provider profiles, masked credentials,
  safe repository identity normalization, clone/pull/sync job enqueueing and
  worker execution.
- SQLite and PostgreSQL storage adapters for the closed Stage 05 MVP baseline.
- HTTP APIs for health, config, modules, jobs/status, scanner/registry and
  repository management.

## Executables

- `thelper` - backend runtime and HTTP service process.
- `thelper-worker` - worker process for queued background jobs.
- `thelper-ctl` - administrative CLI for storage diagnostics, configuration
  import, reload, module restart and controlled database migration.

Common commands:

```text
go run ./cmd/thelper -listen 127.0.0.1:8080 -storage-provider sqlite -storage-dsn .artifacts/dev/sqlite/t-helper.db
go run ./cmd/thelper-worker -storage-provider sqlite -storage-dsn .artifacts/dev/sqlite/t-helper.db -concurrency 1
go run ./cmd/thelper-ctl -storage-provider sqlite -storage-dsn .artifacts/dev/sqlite/t-helper.db providers
go run ./cmd/thelper-ctl -config config.example.json -ignore .t-helper.ignore -reconfigure
go run ./cmd/thelper-ctl -reload
go run ./cmd/thelper-ctl -restart global-scanner
go run ./cmd/thelper-ctl -migrate-db
```

When using built binaries, replace `go run ./cmd/<name>` with the binary name.

Install the application and its pinned scanner toolchain into `~/.local/bin`:

```text
make install
```

`make install` downloads TFLint 0.63.1 and Trivy 0.71.2 from their official
GitHub releases, verifies the pinned checksum manifests and archive SHA-256
digests, and installs them next to the t-helper executables. Override the target
with `PREFIX=/custom/prefix make install`. Terraform is supplied by the user and
must be version 1.15.0 or newer.

## HTTP API

Stage 05 exposes the following runtime API surface:

- `GET /api/health`
- `GET /api/config`, `PUT /api/config`
- `GET /api/modules`, `POST /api/modules/reload`,
  `POST /api/modules/restart`
- `GET /api/jobs`, `GET /api/jobs/{id}`
- `GET /api/status`, `GET /api/status/workflows`,
  `GET /api/status/workflows/{job_group_id}`, `GET /api/status/jobs/{job_id}`,
  `GET /api/status/workers`
- `GET /api/root-paths`, `PUT /api/root-paths`
- `POST /api/scans`, `GET /api/scans/{job_id}`
- `GET /api/projects`, `GET /api/projects/{id}`,
  `GET /api/projects/{id}/links`
- `POST /api/project-scans` lifecycle guard for future Stage 06 scans
- `GET /api/repos`, `GET /api/repos/{id}`
- `GET /api/repo-provider-instances`, `PUT /api/repo-provider-instances`
- `GET /api/repo-credentials`, `PUT /api/repo-credentials`
- `POST /api/repos/clone`, `POST /api/repos/pull`,
  `POST /api/repos/sync`
- `GET /api/ignore-rules`, `PUT /api/ignore-rules`
- `GET /api/environments`, `GET /api/environments/{id}`
- `GET /api/workspaces`, `GET /api/workspaces/{id}`

The canonical contracts are documented in [docs/en/api.md](docs/en/api.md) and
[docs/en/interfaces.md](docs/en/interfaces.md).

## Configuration Model

After the first import, the database is the source of truth for runtime
configuration and working data.

Import files:

- `config.json`
- `.t-helper.ignore`

[config.example.json](config.example.json) is a valid reference input for
`thelper-ctl -reconfigure`.

Important behavior:

- `thelper-ctl -reconfigure` validates config strictly, imports config and
  ignore rules into the database, and does not start or restart the service.
- `thelper-ctl -reload` applies reloadable settings from the database and
  reports settings that require a restart.
- `thelper-ctl -restart <module>` restarts one module and updates
  `module_states`.
- `external_databases` prepares a migration target. Runtime storage is switched
  only after successful `thelper-ctl -migrate-db`.
- Raw secrets are rejected. Use `secretref://...` references for secret values.

## Storage And Migrations

- Go module: `github.com/artBass-rip/t-helper`, Go `1.23`.
- Current storage providers: `sqlite`, `postgres`.
- External provider name `postgresql` is normalized to internal `postgres`.
- Migrations are dialect-specific and synchronized by logical version under
  `internal/storage/migrations/{sqlite,postgres}`.
- Stage 01 schema: `system_metadata` plus migration metadata.
- Stage 02 schema: `config_entries`, `storage_profiles`,
  `storage_provider_settings`, `module_states`, imported system
  `ignore_rules`.
- Stage 03 schema: `jobs`, `job_locks`, `job_events`,
  `workflow_statuses`.
- Stage 04 schema: `root_paths`, `projects`, `project_links`, minimal
  `repositories`, `environments`, `workspaces`.
- Stage 05 schema: provider instances, repository credentials, repository
  operation reservations/indexes and repository manager hardening.
- MySQL and MSSQL are roadmap targets for Stage 10, not current runtime
  adapters.

## Scanner Behavior

- The global scanner detects Terraform projects by `*.tf` filenames.
- The scanner does not parse Terraform source contents during discovery.
- Symlinked directories and `.git/` directories are skipped in Stage 04.
- Traversal stops below a discovered Terraform project.
- Stage 04 ignore matching is exclude-only. `!pattern` rules are preserved but
  not applied until full `.gitignore` semantics are implemented in a later
  hardening stage.
- Missing projects are tracked without merging separate project rows.
- Projects from the same Git repository are related with `same_repository`
  project links.

## Local Development

Recommended checks:

```text
make test
go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl
docker compose --profile offline -f docker-compose.test.yml run --rm test-runner
```

PostgreSQL contract tests run when `THELPER_POSTGRES_DSN` is set. SQLite tests
run by default.
Use `make race` for manual or nightly race detector checks.

## Documentation

- [docs/en/requirements.md](docs/en/requirements.md) - functional and
  non-functional requirements.
- [docs/en/architecture.md](docs/en/architecture.md) - architecture, modules,
  deployment modes and runtime flow.
- [docs/en/interfaces.md](docs/en/interfaces.md) - CLI, backend API, configuration
  and global scanning behavior.
- [docs/en/api.md](docs/en/api.md) - closed Stage 05 HTTP API baseline, future
  endpoint contracts and response schemas.
- [docs/en/configuration.md](docs/en/configuration.md) - `config.json`,
  `.t-helper.ignore`, reloadability and validation.
- [docs/en/development.md](docs/en/development.md) - local development and test
  contract.
- [docs/en/code-optimization.md](docs/en/code-optimization.md) - completed
  optimizations, quality gate and remaining optimization backlog.
- [docs/en/github-pages.md](docs/en/github-pages.md) - bilingual GitHub Pages
  structure and deployment workflow.
- [docs/en/local-dev-environment.md](docs/en/local-dev-environment.md) -
  Docker-based local environment.
- [docs/en/data-model.md](docs/en/data-model.md) - entities, relationships and
  storage invariants.
- [docs/en/payload-schemas.md](docs/en/payload-schemas.md) - versioned JSON
  payload/result contracts.
- [docs/en/access-control.md](docs/en/access-control.md) - auth, SCIM, RBAC and
  authorization matrix.
- [docs/en/frontend-ui-contract.md](docs/en/frontend-ui-contract.md) - Stage 08 Web
  UI and local GUI contract.
- [docs/en/roadmap.md](docs/en/roadmap.md) - implementation stages and acceptance
  criteria.
- [docs/en/adr/](docs/en/adr/) - architecture decision records.
- [docs/en/implementation-specs/](docs/en/implementation-specs/) - stage-level
  implementation specs.
- [CHANGELOG.md](CHANGELOG.md) and [CHANGELOG.ru.md](CHANGELOG.ru.md) -
  change history.

## Roadmap Status

- Stage 00: completed delivery contract.
- Stage 01: completed backend/storage foundation.
- Stage 02: completed persisted config, module lifecycle and singleton
  runtime.
- Stage 03: completed jobs, workers and status foundation.
- Stage 04: completed scanner and registry MVP.
- Stage 05: completed repository manager MVP: generic Git plus GitHub,
  clone/pull/sync jobs, provider profiles, masked credentials, path safety,
  repository enrichment and operation serialization.
- Stage 06A/06B: external toolchain profiles, project scanner and security
  validator MVP.
- Stage 07: auth, RBAC, SCIM contract/stub and audit.
- Stage 08: React/TypeScript Web UI and local Tauri GUI.
- Stage 09-15: observability hardening, storage adapter expansion, security
  policy packs, admin UI hardening, SCIM full sync, repository webhooks and
  distributed deployment.

## Security And Privacy Assumptions

- Runtime configuration and working data use the database as source of truth.
- `GET /api/health` is safe for unauthenticated local discovery and must not
  expose secrets, DSNs, users, object-scoped details or filesystem paths.
- Secret values are represented as `secretref://...` references and must not be
  persisted or returned as plaintext.
- Global scan does not send source code to external services.
- Findings and source code are not sent to external SaaS services by the
  planned local security stack.
