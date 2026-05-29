# ADR 0007: Implementation repository layout and migrations

## Status

Accepted.

## Decision

The initial code scaffold uses one Go module with three executable entrypoints:

- `cmd/thelper` - backend runtime and HTTP API process;
- `cmd/thelper-worker` - background worker process;
- `cmd/thelper-ctl` - administrative CLI.

The repository layout is:

```text
cmd/
  thelper/
  thelper-worker/
  thelper-ctl/
internal/
  app/
    server/
    worker/
    ctl/
  httpapi/
  config/
  storage/
    migrations/
      sqlite/
      postgres/
      mysql/
      mssql/
    sqlite/
    postgres/
    mysql/
    mssql/
  domain/
    projects/
    repositories/
    jobs/
    modules/
    auth/
    security/
  services/
    scanner/
    repomanager/
    projectscanner/
    securityvalidator/
    statusmonitor/
  runtime/
  authz/
  audit/
  log/
  testkit/
web/
docs/
```

`cmd/*` packages only perform bootstrap and wiring. HTTP handlers call application services and do not contain domain logic. Domain packages do not depend on concrete storage adapters. Worker handlers use the same application services as API/CLI entrypoints where possible.

SQL migrations are stored under dialect-specific directories:

```text
internal/storage/migrations/
  sqlite/
  postgres/
  mysql/
  mssql/
```

Migration naming uses synchronized, monotonically increasing six-digit prefixes in every dialect directory:

```text
sqlite/000001_create_system_tables.sql
postgres/000001_create_system_tables.sql
mysql/000001_create_system_tables.sql
mssql/000001_create_system_tables.sql
```

If a logical migration version exists for one supported dialect, the same version must exist for every supported dialect in that release line. SQL content may differ by dialect.

After a migration is merged and may have been applied, it is append-only. Changes must be delivered by a new migration.

Seed data for system permissions, system roles, role permissions and default module registry belongs to migrations or migration-adjacent seed steps that are versioned with the schema.

## Rationale

The layout keeps runtime, worker and CLI bootstrap separate while sharing domain and application code. Keeping adapters below `internal/storage` prevents storage-specific decisions from leaking into domain logic.

Migration ordering must be deterministic before Stage 01, because storage contracts are the foundation for config, jobs, registry, auth and findings.

The project uses one logical schema contract with multiple physical implementations. This allows PostgreSQL, MySQL and MSSQL to use their native SQL features without forcing SQLite compatibility into production-oriented schemas.

## Consequences

- Stage 01 scaffolding follows this package layout unless a superseding ADR is accepted.
- Migration tests must run against a clean SQLite database and a clean PostgreSQL database.
- Stage 10 adds the same migration contract tests for clean MySQL and MSSQL databases.
- Dialect-specific migrations are allowed and expected.
- Storage compatibility is verified by shared behavior/contract tests across adapters, not by comparing DDL text.
- Critical constraints must be enforced in storage where supported and duplicated in application validation where a backend cannot enforce them consistently.
