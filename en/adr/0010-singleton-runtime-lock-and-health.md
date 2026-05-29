# ADR 0010: Singleton runtime lock and health

## Status

Accepted.

## Decision

Local mode uses a runtime lock file plus an HTTP health probe to enforce a single active `t-helper` runtime.

The runtime lock file contains:

- `schema_version`;
- `instance_id`;
- `pid`;
- `host`;
- `api_listen_address`;
- `started_at`;
- `updated_at`;
- `config_database_fingerprint`.

Default lock file location:

```text
<runtime_dir>/thelper-runtime.lock
```

`runtime_dir` is resolved from explicit runtime config first, then platform defaults. The implementation must keep the path local to the current user or service account unless a deployment package explicitly configures a shared service runtime directory.

Startup behavior:

1. `thelper` tries to acquire an exclusive process lock for the runtime lock file.
2. If no active lock exists, `thelper` writes lock metadata and starts normally.
3. If a lock exists, `thelper` reads the lock metadata and probes the recorded health endpoint.
4. If PID and health probe indicate a live runtime, the second process exits without starting another runtime.
5. If PID is gone and health probe fails, the lock is stale and may be replaced.
6. If PID and health disagree, startup fails closed and reports a diagnostic error rather than risking split-brain.

Health endpoint:

```text
GET /api/health
```

The health response must include `instance_id`, `mode`, `database_fingerprint`, `started_at` and a basic readiness state. Tauri GUI and local tools use the lock file plus `/api/health` to discover and verify an existing runtime.

Stage 01 must introduce the final `health_status.v1` response shape from
`docs/en/api.md`. Stage 02 adds runtime lock acquisition, stale lock handling and
lock/probe semantics to that endpoint without a breaking response schema change.

`config_database_fingerprint` identifies the runtime database/config target without exposing credentials. A second runtime must not attach to a different local database while an active runtime lock exists.

## Rationale

A lock file alone is not enough after crashes, and a health endpoint alone does not tell local clients where to connect before discovery. Combining both gives deterministic startup behavior for `thelper`, Tauri GUI and `thelper-ctl`.

Failing closed on ambiguous lock/health state avoids split-brain local state and conflicting writes to the runtime database.

## Consequences

- Stage 01 implements the safe unauthenticated `/api/health` endpoint with the final `health_status.v1` DTO shape.
- Stage 02 implements lock acquisition, stale lock handling and singleton runtime discovery semantics on top of the existing `/api/health` endpoint.
- Tauri GUI discovery uses the runtime lock file and verifies it through `/api/health`.
- Starting a second `thelper` against a live runtime exits without creating a second active process.
- Tests must cover normal startup, second-process detection, stale lock replacement and PID/health mismatch failure.
