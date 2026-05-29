# ADR 0019: Code Quality Baseline And Route Registration

## Status

Accepted.

## Context

После Stage 05 runtime содержит стабильный набор backend routes, jobs lifecycle
paths и storage-backed stores. Предыдущая реализация `httpapi.New` принимала
`optionalHandlers ...any` и регистрировала routes через `type-switch`. Такой
подход позволял передать неподдержанный handler без compile-time ошибки: он
молчаливо игнорировался.

Store-слой также содержит повторяющийся паттерн:

```text
BeginTx -> defer Rollback -> business writes -> Commit
```

Паттерн корректен, но при росте store-файлов усложняет чтение изменяемых
lifecycle paths.

## Decision

HTTP route composition использует явный compile-time контракт:

```go
type RouteRegistrar interface {
	RegisterRoutes(chi.Router)
}
```

Каждый HTTP handler регистрирует собственные routes рядом со своими методами.
`httpapi.New` отвечает только за создание router, common middleware и вызов
`RegisterRoutes`.

Для транзакций добавлен helper:

```go
func WithTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) error
```

Он централизует rollback/commit поведение и оставляет возможность передавать
`sql.TxOptions` для hot paths.

Локальный quality gate зафиксирован в `Makefile`:

- `make test`: `gofmt` check, `go vet ./...`, `go test ./...`;
- `make race`: `go test -race ./...`.

Performance work должен сопровождаться benchmarks или другим измеримым
baseline. Первые benchmarks добавлены для `jobs.Store.ClaimNext` и
`jobs.Store.RefreshWorkflowStatus`.

## Consequences

- Unsupported HTTP handler type больше нельзя передать в `httpapi.New` без
  compile-time ошибки.
- Route ownership виден локально в handler-файлах, а общий route smoke-test
  защищает от случайной потери endpoint.
- `storage.WithTx` используется в новых или сильно изменяемых местах; массовый
  механический рефакторинг существующего store-кода не требуется.
- `make test` становится рекомендуемой командой перед PR/commit.
- Stage 09 optimization backlog должен ссылаться на измерения, а не только на
  интуицию.

## Non-goals

- Полный SQL dialect abstraction не входит в это решение и остаётся отдельным
  backlog item.
- Разделение крупных store-файлов не выполняется механически без изменений
  соответствующей области.
- `WithTx` не скрывает domain-specific retry, isolation или lock strategy.
