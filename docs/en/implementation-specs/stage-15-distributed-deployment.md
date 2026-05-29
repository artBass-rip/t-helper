# Stage 15: Distributed Deployment

## Goal

Prepare the system for multi-node and HA deployment without breaking domain contracts and without diverging from monolithic mode.

## Inputs

- `docs/en/architecture.md`
- `docs/en/roadmap.md`
- `docs/en/technology-stack.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/access-control.md`
- `docs/en/implementation-specs/stage-09-runtime-observability-hardening.md`

## Scope

- extracting `global-scanner`;
- extracting `project-scanner`;
- extracting `repository-manager`;
- extracting `security-validator`;
- extracting `auth`;
- optional extraction of `status-monitor`;
- inter-module contracts;
- service discovery and health model;
- worker groups, queues, locks ownership;
- HA topology.

## Non-goals

- changing MVP product scenarios;
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
- Platform criteria are defined in `docs/en/roadmap.md`.

## Risks

- distributed extraction before runtime hardening multiplies runtime complexity;
- informal inter-module contracts diverge from monolith;
- races around jobs/locks in multi-node setup.
