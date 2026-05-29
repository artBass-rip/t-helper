# ADR 0001: Go backend HTTP stack

## Status

Accepted.

## Decision

Backend runtime, CLI и worker components реализуются на `Go`.

HTTP API реализуется на стандартном `net/http` с lightweight router/middleware stack. Recommended router: `chi`.

## Rationale

- Go подходит для single-binary on-premise delivery.
- `net/http` сохраняет backend framework-neutral.
- `chi` хорошо ложится на REST API, middleware и `httptest`.
- Router не должен протекать в доменную логику.

## Consequences

- Handlers вызывают application services.
- API contracts остаются в `docs/ru/api.md`.
- Framework-specific code должен быть локализован в HTTP adapter layer.
