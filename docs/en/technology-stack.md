# Technology Stack

This document records the baseline implementation stack for `t-helper` code
scaffolding and later roadmap stages.

## Backend

The backend is implemented in `Go`.

Scope:

- runtime service `thelper`;
- worker process `thelper-worker`;
- administrative CLI `thelper-ctl`;
- backend HTTP API;
- module lifecycle and jobs framework;
- status-monitor and aggregate status read models;
- storage abstraction and adapters;
- orchestration of local toolchain commands for scanning and repository operations.

Backend implementation requirements:

- domain logic must not depend on a specific storage backend;
- provider-specific integrations must be isolated in pluggable modules/libraries behind internal interfaces;
- API contracts must match `docs/en/api.md`;
- CLI contracts must match `docs/en/interfaces.md`;
- background jobs must use documented payload schemas from `docs/en/payload-schemas.md`;
- outbound calls for the security stack are not allowed, except for explicitly configured repository/provider integrations.

## Storage

MVP storage stack:

- `SQLite` - default internal SQL-like storage for local setup;
- `PostgreSQL` - default external storage for server setup.

Platform storage targets:

- `SQLite`;
- `PostgreSQL`;
- `MySQL`;
- `MSSQL`.

Managed engine compatibility:

- Aurora PostgreSQL through the PostgreSQL adapter;
- Aurora MySQL through the MySQL adapter;
- Babelfish for Aurora PostgreSQL is not a substitute for the native MSSQL adapter.

Requirements:

- both MVP storage backends use a SQL-like model and support migrations;
- the logical data model and storage interfaces are the same for all supported adapters;
- SQL migrations use dialect-specific SQL with synchronized logical migration versions;
- `MySQL` and `MSSQL` are added in Stage 10 Storage Adapter Expansion as first-class SQL adapters.

Database providers are implemented as pluggable storage adapter libraries selected by configuration. Provider-specific SQL, connection handling and health checks must not leak into domain services or HTTP handlers.

## Jobs and Workers

Background jobs are executed by separate worker processes.

Base components:

- `thelper` - API/runtime process; creates jobs and manages configuration, module states and HTTP API;
- `thelper-worker` - separate worker process; atomically claims queued jobs through storage-level leases, executes job handlers and stores result payloads;
- `thelper-ctl` - administrative CLI; creates lifecycle/config jobs or calls the documented backend API.

Invariants:

- API/CLI do not execute long-running jobs inline;
- jobs are persisted in the database and have documented payload/result schemas;
- job leases define ownership of a specific job by a worker process;
- `job_locks` serialize conflicting business operations across worker processes;
- workers support heartbeat, lease expiry recovery and retry/backoff;
- jobs publish status events/metrics aggregated by `status-monitor`;
- UI and internal services read aggregate status from status-monitor read models;
- multiple worker processes can run in parallel when their lock keys do not conflict;
- distributed deployment extends this model but does not change jobs storage contracts.

## Frontend

The unified frontend surface is implemented with:

- `React`;
- `TypeScript`;
- `Vite`;
- `TanStack Router`;
- `TanStack Query`;
- `Zod`;
- `React Hook Form`;
- `Ant Design`.

Scope:

- `Web UI`;
- shared UI codebase for the local `GUI`;
- MVP read/operate scenarios;
- full MVP administrative UI delivered in Stage 08, with Stage 12 reserved for admin hardening and platform-only administrative extensions.

Frontend implementation requirements:

- `Web UI` and `GUI` use one documented backend API;
- the frontend does not introduce or require frontend-only backend endpoints;
- API DTOs are validated at the client boundary through typed schemas;
- long-running operations are displayed through `jobs` and documented job references;
- UI must be suitable for dense operational scenarios: tables, filters, forms, statuses, findings, RBAC and audit views.
- route map, navigation model and operational density rules must follow [`frontend-ui-contract.md`](frontend-ui-contract.md).

## Desktop GUI

The local desktop GUI is implemented with `Tauri` using the same
React/TypeScript codebase.

GUI requirements:

- `GUI` works only locally;
- remote access to `GUI` is unsupported;
- system interaction goes through the same backend API as `Web UI`;
- GUI-specific behavior must not change backend contracts;
- Tauri packaging/signing and local runtime discovery policy must follow [`frontend-ui-contract.md`](frontend-ui-contract.md).

## Singleton Runtime Policy

Only one `t-helper` runtime instance may run in a single local installation.

Rules:

- if `thelper` is already running for the `Web UI`, the Tauri GUI connects to the existing local runtime;
- if `thelper` is not running yet, the Tauri GUI starts a local `thelper`, after which the `Web UI` connects to the same runtime;
- a repeated `thelper` start must discover the existing runtime through the lock/health mechanism and exit without creating a second active process;
- `Web UI` and `GUI` always work with one backend API and one runtime database.

## Stack Invariants

- Backend: `Go`.
- Web frontend: `React + TypeScript + Vite`.
- Routing: `TanStack Router`.
- Server state: `TanStack Query`.
- Runtime/client validation: `Zod`.
- Forms: `React Hook Form`.
- UI component system: `Ant Design`.
- Desktop shell: `Tauri`.
- Internal local storage: `SQLite`.
- External server storage: `PostgreSQL`.
- Platform storage targets: `SQLite`, `PostgreSQL`, `MySQL`, `MSSQL`.
- Background execution: separate `thelper-worker` processes.
