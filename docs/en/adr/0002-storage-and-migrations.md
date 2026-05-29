# ADR 0002: Storage and migrations

## Status

Accepted.

## Decision

MVP storage stack:

- `SQLite` - default internal SQL-like storage for local setup;
- `PostgreSQL` - default external storage for server setup.

Platform storage targets:

- `SQLite`;
- `PostgreSQL`;
- `MySQL`;
- `MSSQL`.

Managed engine compatibility:

- Amazon Aurora PostgreSQL is supported through the `PostgreSQL` adapter and migration dialect.
- Amazon Aurora MySQL is supported through the `MySQL` adapter and migration dialect.
- Aurora is an engine flavor, not a separate storage provider.
- Babelfish for Aurora PostgreSQL is not considered equivalent to the `MSSQL` adapter unless a separate compatibility ADR is accepted.

PostgreSQL adapter uses `pgx`.

Storage providers are implemented as pluggable adapter libraries behind the storage abstraction. Provider-specific connection handling, migrations and query semantics must remain inside the adapter package.

SQL migrations use a migration framework compatible with dialect-specific migration sets for `SQLite`, `PostgreSQL`, `MySQL` and `MSSQL`; recommended default: `goose` if it satisfies the required dialect support in implementation.

Migration versions are shared across dialects, but migration SQL is dialect-specific. The goal is one logical schema contract, not one portable SQL file.

Embedded key-value storage is not part of the MVP storage stack.

## Rationale

- Local storage should stay SQL-like to keep migrations, constraints and query model close to server storage.
- `SQLite` is simpler operationally than an embedded key-value store for this data model.
- `PostgreSQL` remains the primary server storage backend.
- `MySQL` and `MSSQL` are common enterprise SQL targets and should use the same logical schema contract through dialect-specific migrations.
- Aurora PostgreSQL and Aurora MySQL are wire-compatible managed engines for the PostgreSQL/MySQL adapters, but production readiness still requires contract tests against Aurora writer endpoints.
- A single cross-dialect SQL file would force the schema down to the weakest common subset and still would not guarantee identical behavior.

## Consequences

- Stage 01 implements `SQLite` and `PostgreSQL` adapters.
- Storage test suites must run against both adapters.
- Stage 10 implements `MySQL` and `MSSQL` adapters unless reprioritized by a later roadmap decision.
- Aurora PostgreSQL/Aurora MySQL compatibility must be validated by running the PostgreSQL/MySQL storage contract suites against Aurora writer endpoints.
- Additional storage adapters should be SQL-compatible unless explicitly approved.
- Contract tests, not identical DDL text, define cross-storage compatibility.
