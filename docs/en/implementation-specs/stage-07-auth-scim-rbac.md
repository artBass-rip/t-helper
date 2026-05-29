# Stage 07: Auth, RBAC, SCIM and Audit

## Goal

Introduce a separate `auth` module that provides local authentication, authorization, RBAC, a SCIM contract/stub and audit logging for backend API security events.

## Inputs

- `docs/en/access-control.md`
- `docs/en/api.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/test-plan.md`
- `docs/en/adr/0014-local-password-hashing.md`

## Scope

- `auth` module;
- local authentication;
- first-run bootstrap admin user flow;
- runtime auth session API: login, logout, current session, password reset and password change;
- external auth provider skeleton;
- pluggable auth provider registry;
- users, groups, memberships;
- roles, permissions, role bindings;
- API authorization enforcement;
- SCIM contract/stub without a full sync workflow;
- `audit_log` for security events.

## Non-goals

- frontend administrative UI;
- enterprise IAM integrations beyond the agreed minimal provider set;
- detailed policy authoring UI.
- full SCIM sync workflow.

## Deliverables

- auth module;
- auth provider libraries for local auth and future external providers;
- persistence model for RBAC and SCIM;
- local credentials persistence with Argon2id PHC hashes;
- `user_sessions` persistence with hash-only opaque session token storage;
- `auth_bootstrap_credentials` persistence for one-time first-run bootstrap display and expiry tracking;
- API enforcement middleware;
- permission seed/migrations;
- SCIM endpoints returning controlled contract/stub behavior where sync is not implemented;
- authorization/audit tests.

## Definition of Done

- all API endpoints check the authorization matrix;
- wildcard permissions are expanded into concrete permissions at seed/migration level;
- runtime does not use wildcard string matching;
- object-scoped access is not exposed through `system.runtime.read`;
- group inheritance works;
- auth providers are implemented as pluggable libraries behind a provider interface;
- unknown auth provider is rejected with a controlled validation error;
- local password hashes use Argon2id PHC strings and are stored outside `users`;
- first-run bootstrap admin is created only for empty auth state, shown once in first UI and stdout, expires after 24 hours and is not recreated automatically;
- successful login creates a session without persisting raw session token material;
- logout revokes the current session;
- successful login triggers rehash when stored Argon2id parameters are weaker than current defaults;
- 5 consecutive failed local password attempts lock credentials for 15 minutes;
- password/reset secret material is never returned by API responses and never written to logs, jobs, events, workflow summaries or audit payloads;
- auth/RBAC operations write audit events;
- SCIM endpoints are present as contract/stub and do not imply completed sync behavior in MVP.

## Deferred non-blocking decisions

- minimal set of external auth providers beyond local auth;
- SCIM sync conflict policy and full sync handler.

## Traceability

- Roadmap: Stage 07.
- Acceptance: `ACC-MVP-016`, `ACC-MVP-017`.
- API: auth users/groups/roles/bindings, SCIM contract/stub, audit.
- Data model: auth/RBAC/SCIM/audit entities, `local_user_credentials`, `password_reset_tokens`, `auth_bootstrap_credentials`.
- ADR: `0012`, `0014`.

## Risks

- late RBAC enforcement will require reworking early handlers;
- ambiguous precedence between system and object roles;
- provider abstraction may expand into enterprise IAM before MVP.
