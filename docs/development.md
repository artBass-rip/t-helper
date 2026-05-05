# Development setup

## Purpose

This document defines the local development contract for Stage 01 scaffolding and storage tests.

The Docker-based developer environment, product/dependency containers, manual
testing profile and OS-family test matrix are defined in
[`local-dev-environment.md`](local-dev-environment.md).

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

## Initial implementation checklist

- Scaffold `cmd/thelper`, `cmd/thelper-worker` and `cmd/thelper-ctl`.
- Add `internal/storage/migrations`.
- Add SQLite migration test coverage.
- Add PostgreSQL migration test coverage gated by `THELPER_POSTGRES_DSN`.
- Keep MySQL and MSSQL migration directories reserved for Stage 10 adapters.
- Seed only Stage 01-owned bootstrap metadata through versioned migrations or
  migration-adjacent seed steps.
- Seed the default module registry in Stage 02 with `module_states`.
- Seed system permissions, roles and role permissions in Stage 07 with the
  auth/RBAC schema.
