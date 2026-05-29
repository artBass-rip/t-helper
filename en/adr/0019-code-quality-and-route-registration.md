# ADR 0019: Code Quality Baseline And Route Registration

## Status

Accepted.

## Context

After Stage 05, the runtime contains a stable set of backend routes, jobs
lifecycle paths, and storage-backed stores. The previous `httpapi.New`
implementation accepted `optionalHandlers ...any` and registered routes through
a `type-switch`. This approach allowed an unsupported handler to be passed
without a compile-time error: it was silently ignored.

The store layer also contains a repeated pattern:

```text
BeginTx -> defer Rollback -> business writes -> Commit
```

The pattern is correct, but as store files grow it makes mutable lifecycle paths
harder to read.

## Decision

HTTP route composition uses an explicit compile-time contract:

```go
type RouteRegistrar interface {
	RegisterRoutes(chi.Router)
}
```

Each HTTP handler registers its own routes next to its methods. `httpapi.New`
is responsible only for creating the router, common middleware, and calling
`RegisterRoutes`.

A helper has been added for transactions:

```go
func WithTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) error
```

It centralizes rollback/commit behavior and preserves the ability to pass
`sql.TxOptions` for hot paths.

The local quality gate is fixed in `Makefile`:

- `make test`: `gofmt` check, `go vet ./...`, `go test ./...`;
- `make race`: `go test -race ./...`.

Performance work must be accompanied by benchmarks or another measurable
baseline. The first benchmarks were added for `jobs.Store.ClaimNext` and
`jobs.Store.RefreshWorkflowStatus`.

## Consequences

- Unsupported HTTP handler types can no longer be passed to `httpapi.New`
  without a compile-time error.
- Route ownership is visible locally in handler files, and the shared route
  smoke test protects against accidental endpoint loss.
- `storage.WithTx` is used in new or heavily changed areas; a broad mechanical
  refactor of existing store code is not required.
- `make test` becomes the recommended command before a PR/commit.
- The Stage 09 optimization backlog must reference measurements, not only
  intuition.

## Non-goals

- A full SQL dialect abstraction is outside this decision and remains a
  separate backlog item.
- Large store files are not split mechanically without changes in the
  corresponding area.
- `WithTx` does not hide domain-specific retry, isolation, or lock strategy.
