# Stage 15: Distributed Deployment

## Цель

Подготовить систему к multi-node и HA deployment без ломки доменных контрактов и без расхождения с монолитным режимом.

## Inputs

- `docs/ru/architecture.md`
- `docs/ru/roadmap.md`
- `docs/ru/technology-stack.md`
- `docs/ru/data-model.md`
- `docs/ru/payload-schemas.md`
- `docs/ru/access-control.md`
- `docs/ru/implementation-specs/stage-09-runtime-observability-hardening.md`

## Scope

- вынос `global-scanner`;
- вынос `project-scanner`;
- вынос `repository-manager`;
- вынос `security-validator`;
- вынос `auth`;
- optional вынос `status-monitor`;
- межмодульные contracts;
- service discovery и health model;
- worker groups, queues, locks ownership;
- HA topology.

## Non-goals

- изменение MVP product scenarios;
- distributed-only user API;
- second configuration source of truth;
- incompatible payload schema changes.

## Deliverables

- distributed runtime architecture package;
- inter-module contracts;
- deployment manifests/instructions;
- HA/failover test suite;
- security model for inter-module calls.

## Definition of Done

- monolithic and distributed modes share API/storage contracts;
- jobs, locks, payload schemas and module states remain compatible;
- retry, timeout and idempotency policies are documented;
- status-monitor ownership is defined for multi-node mode;
- inter-module auth/auditability is formalized;
- failover tests pass.

## Hard dependencies

- Stage 09 runtime and observability hardening.

## Conditional dependencies

- Stage 10 storage adapter expansion is required only for distributed release scopes that include MySQL/MSSQL or additional adapters.
- Stage 11 security tooling is required only for distributed release scopes that include those additional tools/policy packs.
- Stage 13 SCIM full sync is required only for distributed release scopes that include full SCIM sync.
- Stage 14 repository webhook sync is required only for distributed release scopes that include webhook ingress.

## Platform blockers

- service discovery mechanism;
- deployment target stack;
- inter-module authentication mechanism;
- queue/worker group ownership model.

## Traceability

- Roadmap: Stage 15.
- Acceptance: `ACC-PLATFORM-005`.
- Platform criteria are defined in `docs/ru/roadmap.md`.

## Риски

- distributed extraction before runtime hardening multiplies runtime complexity;
- informal inter-module contracts diverge from monolith;
- races around jobs/locks in multi-node setup.
