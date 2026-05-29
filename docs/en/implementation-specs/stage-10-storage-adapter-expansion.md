# Stage 10: Storage Adapter Expansion

## Goal

Expand the platform release storage stack to `MySQL`, `MSSQL` and other approved SQL-compatible adapters without changing the logical data model or API contracts.

## Inputs

- `docs/en/roadmap.md`
- `docs/en/test-plan.md`
- `docs/en/development.md`
- `docs/en/architecture.md`
- `docs/en/technology-stack.md`
- `docs/en/data-model.md`
- `docs/en/adr/0002-storage-and-migrations.md`
- `docs/en/adr/0007-implementation-repository-layout-and-migrations.md`

## Scope

- `MySQL` adapter;
- `MSSQL` adapter;
- additional SQL-compatible adapters if approved for the platform release;
- dialect-specific migrations with synchronized logical migration versions;
- storage provider registry extension;
- shared storage contract suite for all target adapters;
- documented storage behavior differences.

## Non-goals

- multi-node orchestration;
- feature changes above storage abstraction;
- incompatible data model changes;
- API contract changes.

## Deliverables

- MySQL storage adapter and migrations;
- MSSQL storage adapter and migrations;
- optional approved adapter implementations;
- cross-storage compatibility tests;
- updated development/test documentation for adapter contract runs.

## Definition of Done

- storage compatibility suite passes for SQLite, PostgreSQL, MySQL and MSSQL;
- migration logical versions are synchronized across supported dialect directories;
- known dialect differences are documented and covered by application-level validation where needed;
- MySQL/MSSQL adapters do not leak provider-specific behavior into domain services or HTTP handlers;
- platform storage acceptance tests pass for target adapters.

## Platform blockers

- approved list of additional SQL-compatible adapters beyond `MySQL` and `MSSQL`.

## Traceability

- Roadmap: Stage 10.
- Acceptance: `ACC-PLATFORM-001`.
- Platform criteria are defined in `docs/en/roadmap.md`.

## Risks

- SQL adapter behavior differences;
- migration drift between dialect directories;
- hidden dependency on PostgreSQL-specific behavior in domain code.
