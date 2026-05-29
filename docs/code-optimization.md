# Code Optimization And Quality Baseline

Дата актуализации: 2026-05-29.

Этот документ фиксирует уже выполненные оптимизации, текущий quality gate и
оставшийся backlog, который должен внедряться итеративно без массовых
механических рефакторингов.

## Текущий статус

Выполнено:

- HTTP route composition переведён с `optionalHandlers ...any` и `type-switch`
  на compile-time контракт `httpapi.RouteRegistrar`.
- Регистрация маршрутов перенесена ближе к handlers через
  `RegisterRoutes(chi.Router)`.
- Добавлен smoke-test полного набора текущих HTTP routes:
  `internal/httpapi/router_test.go`.
- Добавлен транзакционный helper `storage.WithTx(ctx, db, opts, fn)` и
  применён в изменяемых lifecycle paths `jobs.Store.Start`,
  `jobs.Store.Complete`, `jobs.Store.Requeue`.
- Добавлены первые benchmarks для hot paths `jobs.Store.ClaimNext` и
  `jobs.Store.RefreshWorkflowStatus`.
- Добавлен локальный quality gate `make test`, который выполняет:
  `gofmt` check, `go vet ./...`, `go test ./...`.
- Добавлен `make race` для ручных/nightly проверок с race detector.

Проверенный baseline после оптимизации:

```text
make test
```

Примечание: часть тестов открывает локальный listener `127.0.0.1:0`, поэтому в
sandboxed окружениях может потребоваться разрешение на bind.

## Принятые проектные решения

- Каноническое решение по HTTP route registration и транзакционному helper:
  [`adr/0019-code-quality-and-route-registration.md`](adr/0019-code-quality-and-route-registration.md).
- Stage 09 остаётся владельцем runtime/scanner performance hardening:
  [`implementation-specs/stage-09-runtime-observability-hardening.md`](implementation-specs/stage-09-runtime-observability-hardening.md).

## Backlog

### P1. SQL dialect helper

Проблема: store-слой всё ещё содержит ветвления по `handle.Provider` для
SQLite/PostgreSQL placeholders, casts и upsert shapes.

Следующий шаг:

- добавить небольшой helper в `internal/storage` или `internal/storage/sqlutil`;
- централизовать placeholders, `IN (...)`, boolean/time/JSON casts и common
  upsert fragments;
- переводить store-файлы по одному, начиная с `jobs` или `scanner`.

### P1. Workflow status refresh load

Проблема: `RefreshWorkflowStatus` вызывается после многих job lifecycle
событий и пересчитывает aggregate read model.

Следующий шаг:

- использовать добавленный benchmark как базовую точку;
- добавить сценарий на 10k jobs;
- после замеров выбрать debounce, dirty-workflow queue или status-monitor
  reconciliation loop.

### P1. Разделение крупных store-файлов

Проблема: `scanner/store.go`, `config/store.go`, `jobs/store.go`,
`repository/store.go` остаются файлами-накопителями.

Следующий шаг:

- разделять файлы по агрегатам только при изменении соответствующей области;
- сохранять package-level API и тесты без изменения публичного поведения;
- начать с `jobs` после введения SQL helper, чтобы не переносить дублирующийся
  dialect code.

### P2. Filesystem scan backpressure

Проблема: крупные root paths могут создавать неровную нагрузку через частые
progress/error events и поэлементные DB writes.

Следующий шаг:

- добавить coarse-grained progress interval;
- проверить batch-upsert для projects;
- ввести scan metrics: directories/sec, DB writes/sec, skipped/error counts.

### P2. Repository operations separation

Проблема: repository handlers совмещают validation, reservation lifecycle,
credential env, filesystem checks и запуск `git`.

Следующий шаг:

- вынести запуск `git` в `GitRunner`;
- вынести reservation lifecycle в helper с cleanup;
- покрыть fake `GitRunner` тестами для ошибок git/credential/reservation.

### P2. PostgreSQL connection pool settings

Проблема: SQLite provider явно ограничивает pool, а PostgreSQL сейчас
использует defaults `database/sql`.

Следующий шаг:

- добавить storage config для max open/idle conns, lifetime и idle time;
- задать безопасные defaults для server/worker;
- логировать фактические pool settings при старте.

### P3. Дополнительные benchmarks и разрез тестов

Следующий шаг:

- добавить benchmarks для `scanner.Store.ListProjects/ListRepositories`,
  scanner traversal и repository reservation conflict path;
- разделять крупные integration test files по endpoint/aggregate при
  ближайших изменениях этих областей;
- общие fixtures выносить в `*_test_helpers.go`.
