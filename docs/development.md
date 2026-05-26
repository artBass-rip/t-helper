# Development setup

## Purpose

This document defines the local development contract for the current Stage 04
scanner/registry baseline and storage tests.

The Docker-based developer environment, product/dependency containers, manual
testing profile and OS-family test matrix are defined in
[`local-dev-environment.md`](local-dev-environment.md).

Stage 04 has delivered the executable backend scaffold, persisted runtime
configuration, module lifecycle, singleton runtime policy, jobs/workers/status,
global scanning, Terraform project discovery and scanner/registry HTTP APIs.
The current local baseline is:

- `go test ./...`;
- `go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl`;
- `docker compose --profile offline -f docker-compose.test.yml run --rm test-runner`.

The Docker `offline` test runner starts PostgreSQL and runs the same Go test
suite with `THELPER_POSTGRES_DSN` set. DSN-backed tests run with `go test -p 1
./...` so destructive contract tests in different packages do not reset the
same test database concurrently.

## PostgreSQL test environment

PostgreSQL tests run when `THELPER_POSTGRES_DSN` is set. SQLite tests run by default.

Recommended local PostgreSQL baseline:

- PostgreSQL version: `16` or `17`
- database: `t_helper_test`
- user: `t_helper`
- password: local-only test password
- host: `127.0.0.1`
- port: `5432` or a project-specific override

Recommended DSN variable:

```text
THELPER_POSTGRES_DSN=postgres://t_helper:local_password@127.0.0.1:5432/t_helper_test?sslmode=disable
```

CI must set `THELPER_POSTGRES_DSN` and run the storage test suite against both SQLite and PostgreSQL.

## Test policy

- SQLite storage tests are mandatory for every local test run.
- PostgreSQL storage tests are mandatory in CI and optional locally unless `THELPER_POSTGRES_DSN` is set.
- PostgreSQL storage tests perform destructive cleanup only against a configured
  test database. The database name must contain `test` or the caller must set
  `THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS=1`.
- MySQL storage tests run when `THELPER_MYSQL_DSN` is set after the MySQL adapter is implemented.
- MSSQL storage tests run when `THELPER_MSSQL_DSN` is set after the MSSQL adapter is implemented.
- Aurora PostgreSQL contract tests run against a writer endpoint through `THELPER_POSTGRES_DSN`.
- Aurora MySQL contract tests run against a writer endpoint through `THELPER_MYSQL_DSN`.
- Migration tests start from an empty database.
- Tests that need destructive database cleanup must operate only on the configured test database.
- Test fixtures must not contain real secrets.

## Migration layout

Migrations are dialect-specific while sharing logical migration versions:

```text
internal/storage/migrations/
  sqlite/
  postgres/
  mysql/
  mssql/
```

The same logical migration number must exist in every dialect directory supported by the current release line. Contract tests verify behavior parity across adapters.

The current release line ships synchronized SQLite/PostgreSQL logical
migration versions:

- `000001_create_system_tables.sql` for Stage 01 system metadata;
- `000002_stage02_config_modules_runtime.sql` for Stage 02 config, storage
  profiles, module state and imported system ignore rules;
- `000003_stage03_jobs_workers_status.sql` for jobs, job locks, job events and
  workflow statuses;
- `000004_stage04_scanner_registry.sql` for root paths, projects, project
  links, minimal repositories, environments and workspaces;
- `000005_stage04_root_path_source.sql` for root path source tracking.

The `mysql` and `mssql` directories are reserved for Stage 10 adapter expansion
and contain no current SQL migrations.

Stage 01 migrations create only:

- `system_metadata`;
- migration metadata managed by the migration framework.

Stage 02 migrations create:

- `config_entries`;
- `storage_profiles`;
- `storage_provider_settings`;
- `module_states`;
- imported system `ignore_rules`.

Stage 03 migrations create:

- `jobs`;
- `job_locks`;
- `job_events`;
- `workflow_statuses`.

Stage 04 migrations create:

- `root_paths`;
- `projects`;
- `project_links`;
- minimal `repositories`;
- `environments`;
- `workspaces`.

Stage 01-04 migrations must not create later-stage tables such as `users`,
`groups`, `security_findings`, `project_scans` or repository operation tables
owned by Stage 05 and later.

## Stage 04 runtime behavior

- `cmd/thelper` applies Stage 01-04 migrations, starts the HTTP runtime and
  exposes `GET /api/health`, `GET/PUT /api/config`, `GET /api/modules`,
  `POST /api/modules/reload`, `POST /api/modules/restart`, `GET /api/jobs`,
  `GET /api/jobs/{id}`, `GET /api/status*` and Stage 04 scanner/registry
  endpoints.
- `GET /api/health` returns `health_status.v1` with `instance_id`, `mode`,
  safe `database_fingerprint`, `started_at`, `readiness` and `schema_version`.
- `database_fingerprint` is derived from safe storage locator components and
  must not expose DSNs, filesystem paths, usernames, passwords or userinfo.
- SQLite runs through a single open DB connection so connection
  local PRAGMA settings remain effective. Stage 01 sets foreign keys, busy
  timeout and WAL for file-backed SQLite databases.
- `cmd/thelper-worker` executes queued jobs through persistent leases,
  heartbeats, retry/backoff and `job_locks`.
- `cmd/thelper-ctl providers` lists the registered Stage 01 storage providers.
- `cmd/thelper-ctl -reconfigure` imports `config.json` and `.t-helper.ignore`
  into Stage 02-owned tables.
- `cmd/thelper-ctl -reload` applies reloadable keys synchronously and updates
  `module_states` when `modules.enabled` changes.
- `cmd/thelper-ctl -restart <module>` restarts one available module
  synchronously and returns a Stage 02 result DTO.
- `cmd/thelper-ctl -migrate-db` promotes a prepared `migration` storage profile
  only after successful schema/data copy.
- Stage 04 scanner endpoints expose root paths, ignore rules, scans, projects,
  project links, repositories, environments and workspaces.
- Global scan detects Terraform projects by `*.tf` filenames and does not
  parse Terraform source contents during discovery.
- Global scan enqueues coalesced `project_discovery` jobs for Git repository
  association without blocking the global scan result.
- Symlinked directories and `.git/` directories are skipped in Stage 04.
- Traversal stops below a discovered Terraform project.
- Stage 04 ignore matching is exclude-only. `!pattern` rules are preserved but
  not applied until full `.gitignore` semantics are implemented in a later
  hardening stage.

## Project Pages

- `docs/index.html` is the default Russian GitHub Pages entrypoint.
- `docs/en.html` is the English language variant.
- Both pages share `docs/pages.css` and `docs/pages.js`.
- `.github/workflows/pages.yml` publishes the `docs` directory through GitHub
  Actions to the `gh-pages` branch.
- Repository Pages settings must use source `Deploy from a branch`, branch
  `gh-pages`, folder `/ (root)`.
- The Pages landing page is documentation/presentation only and is separate
  from the Stage 08 React/Tauri frontend deliverable.

## Secret references in development

Development configuration should use `secretref://env/...` for sensitive values.

Example:

```json
{
  "external_databases": {
    "username": "secretref://env/THELPER_POSTGRES_USER",
    "password": "secretref://env/THELPER_POSTGRES_PASSWORD"
  }
}
```

Resolved secret values must not be written to `config_entries`, logs, job payloads, job events, result payloads or audit records.

## Stage 01 implementation checklist

- Scaffold `cmd/thelper`, `cmd/thelper-worker` and `cmd/thelper-ctl` - done.
- Add `internal/storage/migrations` - done.
- Add SQLite migration test coverage - done.
- Add PostgreSQL migration test coverage gated by `THELPER_POSTGRES_DSN` - done.
- Keep MySQL and MSSQL migration directories reserved for Stage 10 adapters - done.
- Seed only Stage 01-owned bootstrap metadata through versioned migrations or
  migration-adjacent seed steps - done through `system_metadata`.
- Seed the default module registry in Stage 02 with `module_states`.
- Seed system permissions, roles and role permissions in Stage 07 with the
  auth/RBAC schema.
