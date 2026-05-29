# Stage 08: Frontend MVP and Local GUI

## Goal

Deliver a unified frontend surface consisting of `Web UI` and local `GUI`, both running on top of one backend API and covering MVP read/operate scenarios and full MVP administrative screens.

## Inputs

- `docs/en/api.md`
- `docs/en/access-control.md`
- `docs/en/technology-stack.md`
- `docs/en/frontend-ui-contract.md`
- `docs/en/test-plan.md`
- `docs/en/adr/0004-frontend-and-tauri-runtime-policy.md`

## Scope

- `Web UI`;
- local `GUI`;
- shared React/TypeScript codebase;
- Vite, TanStack Router, TanStack Query, Zod, React Hook Form, Ant Design;
- screens and flows for MVP read/operate scenarios;
- full administrative UI screens for auth/RBAC/configuration/security rule sets in both `GUI` and `Web UI`;
- login/logout/current session/password flows through the documented runtime auth API;
- Tauri local runtime discovery/startup;
- shared route tree, navigation model and operational density from `docs/en/frontend-ui-contract.md`;
- integration only with documented backend API.

## Non-goals

- full SCIM sync management beyond Stage 07 contract/stub;
- advanced platform-only admin hardening owned by Stage 12;
- frontend-only backend endpoints;
- remote access to GUI.

## Deliverables

- shared frontend app;
- Web UI build;
- Tauri GUI shell;
- typed API client with Zod schemas;
- e2e and contract tests;
- singleton runtime UI integration.
- auth session UI and access-denied states.
- route map/navigation/density implementation according to `docs/en/frontend-ui-contract.md`.

## Definition of Done

- frontend uses only documented API endpoints;
- Web UI and GUI share one codebase;
- Web UI and GUI use the same route tree from `docs/en/frontend-ui-contract.md` unless a route is explicitly marked local-only or unavailable by target backend API scope;
- list-heavy operational screens follow compact, table-first density rules from `docs/en/frontend-ui-contract.md`;
- GUI works only locally;
- long-running operations display job/status/result;
- clone workflow includes protocol selector near URL field;
- repository integration UI supports GitKraken-like provider profiles, multiple hosts per provider and multiple credentials per host;
- credential UI accepts only secret references and shows masked values;
- scoped findings and global security view are supported;
- full MVP administrative screens for auth/RBAC/configuration/security rule sets are implemented in both `Web UI` and local `GUI`, unless a documented local-only restriction applies;
- auth states and access denial are visible in UI;
- frontend uses `GET /api/health` only for safe local runtime discovery/readiness and uses authenticated status endpoints for detailed runtime state;
- Tauri development/release packaging state is documented before release artifact publication.

## Stage entry decisions

- Stage 08 UI delivery contract accepted, including route map, navigation model and operational density rules;
- Tauri runtime discovery/start policy accepted for supported local OS targets.

## Stage exit decisions

- packaging/signing policy accepted before release artifact publication;
- update/distribution channel policy documented or explicitly deferred before release artifact publication.

## Traceability

- Roadmap: Stage 08.
- Acceptance: `ACC-MVP-011`, `ACC-MVP-012`, `ACC-MVP-015`, `ACC-MVP-021`, `ACC-PLATFORM-002`, `ACC-PLATFORM-003` where the corresponding backend APIs are part of the MVP release.
- API: all MVP read/operate endpoints.
- UI contract: `docs/en/frontend-ui-contract.md`.
- ADR: `0004`.

## Risks

- frontend compensates for backend contract gaps;
- GUI and Web UI diverge;
- frontend starts before Stage 05-07 APIs stabilize.
