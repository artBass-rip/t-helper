# Frontend UI Delivery Contract

## Purpose

This document records the Stage 08 UI contract for the unified `Web UI` and
local `GUI`. The goal is to prevent divergence in route tree, navigation model,
density and runtime assumptions between the browser UI and the Tauri shell.

`Web UI` and `GUI` use one React/TypeScript codebase, one typed API client and
one documented backend API. Differences are allowed only where the local nature
of the `GUI` explicitly affects runtime discovery/startup.

## Route Map

Top-level route tree for the Stage 08 MVP:

```text
/projects
/projects/:id
/repositories
/repositories/:id
/scans
/project-scans/:id
/findings
/jobs
/modules
/config
/audit
/auth/users
/auth/groups
/auth/roles
/auth/role-bindings
```

Stage 08 includes full MVP administrative screens for auth, RBAC,
configuration and security rule sets when their backend APIs are part of the
MVP release. Stage 12 hardens and extends these administrative surfaces for
platform-only capabilities such as full SCIM sync management, advanced tool
profile administration and additional release-scope controls.

Route rules:

- `Web UI` and `GUI` use one route tree;
- a route available in the `Web UI` must be available in the `GUI` for the same release scope, unless explicitly marked local-only or unavailable by backend API scope;
- frontend-only backend routes are forbidden;
- object detail routes use tabs for subviews instead of separate unrelated page shells;
- query parameters may hold filters, sorting, pagination cursors and selected tabs when this improves shareability/debugging.

## Navigation Model

- left sidebar for top-level operational areas;
- content header with title, object identity, status and primary actions;
- tabs for object detail subviews;
- tables for list-heavy resources;
- drawers or modals for focused create/edit flows;
- confirmation dialogs for operations that create jobs or change runtime state;
- long-running operations show `job_id`, status, latest event and links to job/status details;
- access-denied and unauthenticated states are first-class UI states, not generic errors.

## Operational Density

Stage 08 UI is an operational tool, not a landing page.

Rules:

- default density is compact;
- list pages are table-first;
- filters are compact, persistent and close to the table they affect;
- cards are allowed for repeated summary items only when they improve scanning;
- nested cards are not allowed;
- no marketing hero or landing page is used as the first screen;
- dashboard summaries must link to concrete operational lists or objects;
- table rows must expose status, ownership/context and primary actions without requiring navigation for every common check;
- forms use documented backend schemas and client-side Zod validation.

## Tauri GUI Policy

- `GUI` is local-only;
- remote access to `GUI` is unsupported;
- `GUI` uses the same API client and route tree as `Web UI`;
- `GUI` discovers runtime through runtime lock file plus `GET /api/health`;
- if runtime is absent, `GUI` may start local `thelper`;
- detailed runtime state uses authenticated `/api/status`, not `/api/health`;
- packaging/signing targets must be listed before release artifact publication;
- development builds may be unsigned if explicitly documented as non-release artifacts.

## Stage 08 Entry Decisions

Stage 08 entry decisions accepted by Stage 00:

- this route map;
- navigation model;
- operational density rules;
- Tauri local runtime discovery/start policy for supported local OS targets.

Changes to these decisions require updating this document together with
`docs/en/implementation-specs/stage-00-delivery-contract.md`, `docs/en/roadmap.md` and
`docs/en/traceability.md` where affected.

## Stage 08 Exit Decisions

Release artifact publication requires:

- supported desktop OS targets listed;
- packaging/signing policy accepted;
- update/distribution channel policy documented or explicitly deferred.
