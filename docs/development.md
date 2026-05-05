# Development setup

## Purpose

This document defines the local development contract for Stage 01 scaffolding and storage tests.

The Docker-based developer environment, product/dependency containers, manual
testing profile and OS-family test matrix are defined in
[`local-dev-environment.md`](local-dev-environment.md).

Stage 01 has delivered the initial executable scaffold. The current local
baseline is:

- `go test ./...`;
- `go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl`;
- `docker compose --profile offline -f docker-compose.test.yml run --rm test-runner`.

The Docker `offline` test runner starts PostgreSQL and runs the same Go test
suite with `THELPER_POSTGRES_DSN` set.

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

Stage 01 currently ships synchronized SQLite/PostgreSQL migration version
`000001_create_system_tables.sql`. The `mysql` and `mssql` directories are
reserved for Stage 10 adapter expansion and contain no Stage 01 SQL migrations.

Stage 01 migrations create only:

- `system_metadata`;
- migration metadata managed by the migration framework.

They must not create later-stage tables such as `config_entries`,
`module_states`, `jobs`, `root_paths`, `projects`, `repositories` or `users`.

## Stage 01 runtime behavior

- `cmd/thelper` applies Stage 01 migrations, starts the HTTP runtime and exposes
  `GET /api/health`.
- `GET /api/health` returns `health_status.v1` with `instance_id`, `mode`,
  safe `database_fingerprint`, `started_at`, `readiness` and `schema_version`.
- `database_fingerprint` is derived from safe storage locator components and
  must not expose DSNs, filesystem paths, usernames, passwords or userinfo.
- SQLite runs through a single open DB connection in Stage 01 so connection
  local PRAGMA settings remain effective. Stage 01 sets foreign keys, busy
  timeout and WAL for file-backed SQLite databases.
- `cmd/thelper-worker` is a buildable scaffold only; job execution starts in
  Stage 03.
- `cmd/thelper-ctl providers` lists the registered Stage 01 storage providers.
  Config import/reload/restart commands start in Stage 02.

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
