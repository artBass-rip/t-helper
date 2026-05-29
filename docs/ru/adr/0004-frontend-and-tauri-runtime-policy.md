# ADR 0004: Frontend, Tauri and singleton runtime policy

## Status

Accepted.

## Decision

Frontend stack:

- `React`;
- `TypeScript`;
- `Vite`;
- `TanStack Router`;
- `TanStack Query`;
- `Zod`;
- `React Hook Form`;
- `Ant Design`.

Desktop GUI uses `Tauri` and the same React/TypeScript codebase.

Only one `t-helper` runtime may be active in a local installation.

Runtime startup policy:

- if `thelper` is already running for `Web UI`, Tauri GUI connects to that runtime;
- if `thelper` is not running, Tauri GUI starts local `thelper`;
- after Tauri starts `thelper`, `Web UI` connects to the same runtime;
- starting a second `thelper` must detect the existing runtime and avoid creating another active process.

## Rationale

- `Web UI` and `GUI` must use one documented backend API.
- A singleton runtime avoids split-brain local state and conflicting local databases.
- Tauri should be a local delivery shell, not a separate backend mode.

## Consequences

- Tauri must implement local runtime discovery/startup logic.
- `thelper` must expose a health/lock mechanism suitable for runtime discovery.
- GUI-specific backend endpoints are not allowed.
