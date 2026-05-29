# ADR 0001: Go backend HTTP stack

## Status

Accepted.

## Decision

The backend runtime, CLI, and worker components are implemented in `Go`.

The HTTP API is implemented on the standard `net/http` package with a lightweight router/middleware stack. Recommended router: `chi`.

## Rationale

- Go is suitable for single-binary on-premise delivery.
- `net/http` keeps the backend framework-neutral.
- `chi` fits REST APIs, middleware, and `httptest` well.
- The router must not leak into domain logic.

## Consequences

- Handlers call application services.
- API contracts remain in `docs/en/api.md`.
- Framework-specific code must be localized in the HTTP adapter layer.
