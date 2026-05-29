# Stage 00 Delivery Contract

## Status

Accepted.

Stage 00 is complete as a documentation and decision stage. It does not create
executable product scaffolding, Go modules, CI workflows, database migrations or
test harnesses. Those deliverables start in Stage 01.

## Canonical Inputs

- `docs/ru/requirements.md`
- `docs/ru/architecture.md`
- `docs/ru/roadmap.md`
- `docs/ru/traceability.md`
- `docs/ru/test-plan.md`
- `docs/ru/configuration.md`
- `docs/ru/development.md`
- `docs/ru/local-dev-environment.md`
- `docs/ru/implementation-specs/stage-00-delivery-contract.md`
- `docs/ru/adr/`
- `config.example.json`

## Accepted Delivery Decisions

The canonical decision register is
`docs/ru/implementation-specs/stage-00-delivery-contract.md`.

Stage 00 confirms these delivery rules for implementation:

- Stage 01 may start with no remaining MVP blockers.
- `projects.repository_id` is the single project-to-repository relationship
  field across storage, API and payload contracts.
- `scanning.global_scan` is the only external config key for global scan roots;
  storage/API/domain code uses `root_paths`.
- `secretref://env/...` is the only MVP secret reference resolver contract.
- Literal secrets are rejected for sensitive persisted settings.
- Resolved secrets never appear in config storage, API responses, jobs, events,
  audit records, workflow summaries, findings or logs.
- MVP public lifecycle uses non-destructive state transitions and no public hard
  delete endpoints.
- Stage-owned migrations introduce physical tables only when the owning stage
  ships behavior and tests for the corresponding invariants.

## Definition of Done

Stage 00 is accepted when:

- every open decision in `docs/ru/roadmap.md` is classified as accepted, deferred,
  platform blocker, stage-local blocker or out of scope in the Stage 00 decision
  register;
- all ADRs required before scaffolding are accepted;
- the Stage 01 repository layout and migration naming contract is fixed in ADR
  0007;
- config key compatibility is fixed in ADR 0008;
- secret resolution is fixed in ADR 0009;
- local SQLite/PostgreSQL development expectations are documented;
- traceability covers every MVP capability and acceptance criterion;
- `config.example.json` uses `scanning.global_scan` and contains no literal
  secret values;
- implementation specs use `ACC-MVP-*` and `ACC-PLATFORM-*` acceptance
  identifiers where applicable;
- Stage 01 can begin without implicit architecture choices.

## Stage 01 Scaffolding Checklist

Stage 01 must create the executable scaffold using the accepted Stage 00
contracts:

- initialize one Go module at the repository root;
- create entrypoints `cmd/thelper`, `cmd/thelper-worker` and
  `cmd/thelper-ctl`;
- create the package layout from ADR 0007 under `internal/`;
- keep `cmd/*` packages limited to bootstrap and wiring;
- add dialect-specific migration directories for `sqlite`, `postgres`, `mysql`
  and `mssql`;
- implement only Stage 01-owned migrations and seed data;
- add shared storage contract tests for SQLite and PostgreSQL;
- gate PostgreSQL tests with `THELPER_POSTGRES_DSN`;
- add `GET /api/health` as safe unauthenticated metadata only;
- reject any alias for `scanning.global_scan`;
- ensure sensitive config paths accept only `secretref://env/...`.

## Implementation Style Guide

Stage 01 and later code must follow these repository-level rules:

- prefer standard library and accepted ADR stack choices before adding
  dependencies;
- keep domain logic out of HTTP handlers, CLI parsing and storage adapters;
- pass `context.Context` through API, CLI, worker and storage boundaries;
- return typed domain/application errors that API and CLI layers translate into
  documented responses;
- keep storage adapters behind interfaces owned by application/domain packages;
- treat migrations as append-only once merged;
- make config import strict: unknown keys, aliases and literal sensitive values
  fail validation before partial persistence;
- keep job payloads stable, versioned and free of raw secrets;
- use table-driven tests for validation, config mapping, storage contracts and
  API error mapping;
- keep local-only assumptions explicit in config, tests and runtime checks.

## CI Skeleton Checklist

Actual CI workflow files are Stage 01 deliverables. The initial CI skeleton must
run these gates once code exists:

- formatting and static checks for Go packages;
- `go test ./...`;
- SQLite storage contract tests;
- PostgreSQL storage contract tests with `THELPER_POSTGRES_DSN`;
- config validation tests for canonical keys and rejected aliases;
- secret masking and literal-secret rejection tests;
- migration tests from empty databases;
- CLI smoke checks for `thelper-ctl`;
- API smoke check for `GET /api/health`;
- artifact upload for test results when the runner supports it.

## Stage 01-03 Backlog

### Stage 01

- Backend HTTP skeleton.
- Storage abstraction.
- SQLite and PostgreSQL adapters.
- Dialect-specific migration runner.
- Health endpoint.
- Local logging and correlation ID foundation.

### Stage 02

- Runtime config import into `config_entries`.
- `module_states` registry and initial module seed.
- `thelper-ctl -reconfigure`.
- `thelper-ctl -reload`.
- `thelper-ctl -restart <module>`.
- Singleton runtime lock and health behavior.

### Stage 03

- `jobs`, `job_locks`, `job_events` and `workflow_statuses`.
- Worker claim, lease, heartbeat, retry and expired lease recovery.
- Worker handlers for `config_reload` and `module_restart`.
- Status monitor foundation.
- Runtime/status APIs required by frontend MVP.

## Validation Notes

Stage 00 validation is documentation-only:

- `config.example.json` must parse as JSON;
- no legacy short repository relationship field contract should remain outside
  historical explanatory text;
- no external config alias should be introduced for `scanning.global_scan`;
- every ADR under `docs/ru/adr/` must have `Status: Accepted`;
- local environment files may exist, but product containers are not required
  until the relevant implementation stages create product code.
