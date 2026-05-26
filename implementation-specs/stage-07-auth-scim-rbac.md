# Stage 07: Auth, RBAC, SCIM and Audit

## Цель

Ввести отдельный модуль `auth`, обеспечивающий локальную аутентификацию, авторизацию, RBAC, SCIM contract/stub и аудит security-событий для backend API.

## Inputs

- `docs/access-control.md`
- `docs/api.md`
- `docs/data-model.md`
- `docs/payload-schemas.md`
- `docs/test-plan.md`
- `docs/adr/0014-local-password-hashing.md`

## Scope

- модуль `auth`;
- локальная аутентификация;
- first-run bootstrap admin user flow;
- runtime auth session API: login, logout, current session, password reset and password change;
- каркас external auth providers;
- pluggable auth provider registry;
- users, groups, memberships;
- roles, permissions, role bindings;
- API authorization enforcement;
- SCIM contract/stub без полноценного sync workflow;
- `audit_log` для security-событий.

## Non-goals

- frontend administrative UI;
- enterprise IAM integrations сверх согласованного минимального набора providers;
- detailed policy authoring UI.
- полноценный SCIM sync workflow.

## Deliverables

- auth module;
- auth provider libraries for local auth and future external providers;
- persistence model для RBAC и SCIM;
- local credentials persistence with Argon2id PHC hashes;
- `user_sessions` persistence with hash-only opaque session token storage;
- `auth_bootstrap_credentials` persistence for one-time first-run bootstrap display and expiry tracking;
- API enforcement middleware;
- permission seed/migrations;
- SCIM endpoints returning controlled contract/stub behavior where sync is not implemented;
- authorization/audit tests.

## Definition of Done

- все API endpoints проверяют authorization matrix;
- wildcard permissions развёрнуты в concrete permissions на seed/migration уровне;
- runtime не использует wildcard string matching;
- object-scoped access не раскрывается через `system.runtime.read`;
- group inheritance работает;
- auth providers реализованы как подключаемые libraries за provider interface;
- unknown auth provider отклоняется controlled validation error;
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

- минимальный набор external auth providers beyond local auth;
- SCIM sync conflict policy and full sync handler.

## Traceability

- Roadmap: Stage 07.
- Acceptance: `ACC-MVP-016`, `ACC-MVP-017`.
- API: auth users/groups/roles/bindings, SCIM contract/stub, audit.
- Data model: auth/RBAC/SCIM/audit entities, `local_user_credentials`, `password_reset_tokens`, `auth_bootstrap_credentials`.
- ADR: `0012`, `0014`.

## Риски

- слишком позднее RBAC enforcement приведёт к переделке early handlers;
- неоднозначность precedence system/object roles;
- provider abstraction может расшириться до enterprise IAM раньше MVP.
