# Stage 13: SCIM Full Sync

## Goal

Implement a full SCIM sync workflow on top of the Stage 07 SCIM contract/stub without breaking API changes.

## Inputs

- `docs/en/access-control.md`
- `docs/en/api.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/test-plan.md`

## Scope

- full SCIM sync handler;
- scheduled and manual SCIM sync;
- conflict policy;
- user/group create/update/deactivate mapping;
- retry and partial failure handling;
- sync audit events;
- status-monitor integration for SCIM jobs.

## Non-goals

- replacing local auth;
- introducing a second identity source of truth outside documented auth/SCIM model;
- frontend-only SCIM workflows.

## Deliverables

- `scim_sync` worker handler beyond contract/stub behavior;
- conflict policy and mapping rules;
- SCIM sync result payloads and events;
- audit and authorization tests;
- operator-visible sync status.

## Definition of Done

- SCIM sync creates, updates and deactivates users/groups according to accepted policy;
- conflicts are represented as controlled sync results and audit events;
- partial failures are visible and retryable;
- Stage 07 SCIM endpoints evolve without breaking route contracts;
- sync jobs use existing worker leases, status-monitor aggregation and audit logging.

## Platform blockers

- SCIM sync conflict policy;
- external SCIM provider minimum contract and mapping rules.

## Traceability

- Roadmap: Stage 13.
- Acceptance: `ACC-PLATFORM-002`, `ACC-PLATFORM-008`.
- API: `GET /api/auth/scim/identities`, `POST /api/auth/scim/sync`.
- Data model: `scim_identities`, auth/RBAC entities, `jobs`, `job_events`.

## Risks

- ambiguous conflict handling changes user access unexpectedly;
- SCIM sync bypasses audit/RBAC invariants;
- partial failures are hidden from operators.
