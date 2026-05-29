# Stage 12: Admin UI Hardening

## Цель

Усилить и расширить administrative UI после Stage 08 full MVP admin screens: SCIM sync management, advanced tool profile administration, platform-only controls and release-specific hardening поверх documented backend API и единого frontend UI contract.

## Inputs

- `docs/ru/frontend-ui-contract.md`
- `docs/ru/api.md`
- `docs/ru/access-control.md`
- `docs/ru/test-plan.md`
- `docs/ru/technology-stack.md`

## Scope

- hardening of Stage 08 administrative UI for auth/RBAC/configuration/security rule sets;
- SCIM identity visibility and sync operation surfaces where backend APIs are available;
- tool profile UI for validate/import/activate/analyze flows if Stage 11 tool profile management is in release scope;
- platform-only administrative controls included in the target release;
- audit/admin flows;
- access-denied and permission-aware UI states.

## Non-goals

- implementing backend auth/RBAC/SCIM sync behavior not already available through documented API;
- frontend-only backend endpoints;
- changing Stage 08 route/navigation/density contract without updating `frontend-ui-contract.md`.

## Deliverables

- hardened admin UI screens in shared Web UI/GUI codebase;
- typed API/Zod schemas for admin endpoints;
- permission-aware navigation and action states;
- frontend contract/e2e tests for admin flows.

## Definition of Done

- admin screens available in `Web UI` are available in local `GUI` for the same release scope;
- admin UI uses only documented backend API endpoints;
- UI follows `docs/ru/frontend-ui-contract.md`;
- permissions are enforced by backend and represented clearly in UI states;
- configuration and rule set changes show resulting jobs/status where applicable.

## Platform blockers

- final admin screen scope for the target platform release;
- whether tool profile management UI is included in this stage or deferred.

## Traceability

- Roadmap: Stage 12.
- Acceptance: `ACC-PLATFORM-002`, `ACC-PLATFORM-003`.
- API: auth/RBAC, config, security rule sets, tool profiles, audit.
- UI contract: `docs/ru/frontend-ui-contract.md`.

## Риски

- frontend compensates for missing backend contracts;
- GUI and Web UI diverge in administrative screens;
- admin UI becomes a separate design system from Stage 08 operational UI.
