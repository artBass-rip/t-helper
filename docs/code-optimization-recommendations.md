# Рекомендации по оптимизации кода

Дата анализа: 2026-05-29.

## Краткий вывод

Кодовая база находится в хорошем базовом состоянии: `go test ./...` и `go vet ./...` проходят без ошибок. Основной резерв оптимизации сейчас не в микропроизводительности, а в снижении сложности DB-слоя, уменьшении повторов между SQLite/Postgres, ограничении роста крупных store-файлов и более явной сборке HTTP/API-компонентов.

Проект содержит около 21.5k строк Go-кода в 70 файлах. Самые дорогие для сопровождения зоны:

- `internal/scanner/store.go` - 1403 строки.
- `internal/config/store.go` - 1271 строка.
- `internal/jobs/store.go` - 1136 строк.
- `internal/repository/store.go` - 1036 строк.
- `internal/httpapi/stage05_repository_test.go` - 1326 строк.

## P1. Убрать дублирование SQL-диалектов из store-слоя

Проблема: во многих store-файлах запросы вручную ветвятся через `handle.Provider == "postgres"` и отдельные `placeholder` helpers. Это встречается в `jobs`, `scanner`, `repository`, `config`, `modules`. Такой подход повышает стоимость каждого изменения схемы: один и тот же запрос приходится держать в голове в двух вариантах.

Рекомендация:

- Вынести минимальный SQL dialect/helper в `internal/storage` или новый `internal/storage/sqlutil`.
- Централизовать:
  - генерацию placeholders;
  - boolean/time expressions;
  - UPSERT/ON CONFLICT шаблоны;
  - batch `IN (...)`;
  - provider-specific casts для timestamp/JSON.
- После этого постепенно переводить store-файлы по одному модулю, начиная с `jobs` и `scanner`.

Ожидаемый эффект: меньше расхождений между SQLite/Postgres, проще ревью миграций, меньше шансов сломать один provider при доработке другого.

## P1. Разделить крупные store-файлы по агрегатам

Проблема: `scanner/store.go`, `config/store.go`, `jobs/store.go`, `repository/store.go` уже стали файлами-накопителями. В них смешаны CRUD, paging, batch-операции, provider-specific SQL, scan helpers и domain-specific правила.

Рекомендация:

- `internal/scanner/store.go` разделить минимум на:
  - `root_paths_store.go`;
  - `projects_store.go`;
  - `repositories_store.go`;
  - `environments_store.go`;
  - `scanner_sql.go`.
- `internal/jobs/store.go` разделить на:
  - `enqueue_store.go`;
  - `claim_store.go`;
  - `events_store.go`;
  - `workflow_store.go`;
  - `jobs_sql.go`.
- `internal/config/store.go` разделить на:
  - `runtime_settings_store.go`;
  - `storage_profile_store.go`;
  - `migration_copy_store.go`;
  - `ignore_rules_store.go`.

Ожидаемый эффект: меньше merge-конфликтов, проще точечные тесты, легче вводить новые storage-provider сценарии.

## P1. Сделать регистрацию HTTP handlers типобезопасной

Проблема: `internal/httpapi/router.go` принимает `optionalHandlers ...any` и регистрирует маршруты через type-switch. Это скрывает ошибку подключения handler-а: неподдержанный тип будет молча проигнорирован.

Рекомендация:

- Заменить `optionalHandlers ...any` на явный `Options` struct или интерфейс вида:
  - `type RouteRegistrar interface { RegisterRoutes(chi.Router) }`.
- Перенести регистрацию маршрутов ближе к самим handlers.
- В тестах проверить набор маршрутов через table-driven smoke-test.

Ожидаемый эффект: compile-time контракт вместо неявного runtime поведения.

## P1. Снизить DB-нагрузку от workflow status refresh

Проблема: `jobs.Runtime` часто вызывает `RefreshWorkflowStatus`: после enqueue, progress events, start/complete/requeue/recover. На больших workflow это может стать лишней DB-нагрузкой, особенно если refresh агрегирует состояние по группе.

Рекомендация:

- Измерить стоимость `RefreshWorkflowStatus` на 1k/10k jobs в группе.
- Для progress-only событий рассмотреть debounce или coalescing.
- Для массовых операций использовать batch refresh по `job_group_id`.
- Добавить метрики длительности refresh и количества refresh на job.

Ожидаемый эффект: стабильнее поведение worker-а на больших scan/repository workflows.

## P2. Стандартизировать транзакционный helper

Проблема: в store-файлах много ручного паттерна `BeginTx`, `defer Rollback`, `Commit`. Он корректен, но повторяется и усложняет чтение бизнес-логики.

Рекомендация:

- Добавить helper вида `WithTx(ctx, db, func(tx *sql.Tx) error) error`.
- Сохранить возможность тонкой настройки `sql.TxOptions` для hot paths.
- Использовать helper только в новых или сильно меняемых участках, чтобы не делать большой механический рефакторинг без пользы.

Ожидаемый эффект: меньше boilerplate, единое поведение rollback/commit, проще читать store-методы.

## P2. Ограничить filesystem scan backpressure и DB writes

Проблема: `globalScanHandler.scanRoot` может обходить дерево параллельно и внутри обхода обновлять результат, seen sets и events. Для Postgres concurrency берется из `workers.concurrency` с cap 64, для SQLite принудительно 1. Это разумно, но без явных лимитов на частоту progress/error events и batch-upsert можно получить неровную нагрузку на больших деревьях.

Рекомендация:

- Добавить coarse-grained progress interval: например, не чаще N секунд или каждые M директорий.
- Проверить, где project upsert выполняется по одному элементу, и заменить на batch там, где это безопасно.
- Ввести scan metrics: directories/sec, DB writes/sec, skipped/error counts.

Ожидаемый эффект: предсказуемая нагрузка при больших root paths, меньше lock contention в SQLite и меньше round-trips в Postgres.

## P2. Явнее разделить repository operations и git execution

Проблема: `internal/repository/handlers.go` совмещает validation, reservation lifecycle, credential env, filesystem checks и запуск `git`. Файл уже 440 строк, а расширение provider-логики увеличит связность.

Рекомендация:

- Вынести запуск git в небольшой `GitRunner` interface.
- Вынести reservation lifecycle в helper, который возвращает cleanup.
- Оставить handler-у orchestration: validate payload -> reserve -> execute -> persist result.
- Добавить тесты на GitRunner fake вместо проверки через реальные команды там, где это возможно.

Ожидаемый эффект: проще тестировать ошибки git/credential/reservation отдельно, легче добавлять новые операции.

## P2. Настроить connection pool для Postgres

Проблема: SQLite provider явно ограничивает pool до одного соединения, а Postgres provider использует defaults `database/sql`. На production-нагрузке defaults могут быть неочевидными и зависят от runtime.

Рекомендация:

- Добавить в storage config параметры:
  - max open conns;
  - max idle conns;
  - conn max lifetime;
  - conn max idle time.
- Дать безопасные defaults для server и worker.
- Логировать фактические pool settings при старте.

Ожидаемый эффект: предсказуемая нагрузка на Postgres и меньше риска исчерпания соединений при росте worker concurrency.

## P3. Сделать benchmark/performance tests для hot paths

Текущие функциональные тесты хорошие, но нет быстрых benchmark-сценариев для оценки регрессий.

Рекомендация добавить benchmarks:

- `jobs.Store.ClaimNext` на очереди 1k/10k jobs.
- `jobs.Store.RefreshWorkflowStatus` на workflow с большим количеством child jobs.
- `scanner.Store.ListProjects/ListRepositories` с paging.
- `scanner` traversal на synthetic filesystem fake.
- `repository` reservation conflict path.

Ожидаемый эффект: рекомендации по индексам и batch-операциям можно будет подтверждать цифрами, а не интуицией.

## P3. Разделить большие интеграционные тесты

Проблема: часть тестовых файлов уже больше 1k строк (`internal/httpapi/stage05_repository_test.go`, `internal/scanner/handlers_test.go`, `internal/repository/store_test.go`). Это усложняет навигацию и локальный запуск точечных сценариев.

Рекомендация:

- Разделить тесты по endpoint/aggregate:
  - provider instances;
  - credentials;
  - clone/pull/sync;
  - reservation conflicts;
  - repository identity.
- Общие fixtures оставить в `*_test_helpers.go`.

Ожидаемый эффект: быстрее локализовать регрессии и проще добавлять новые случаи.

## P3. Добавить легкие quality gates

Сейчас `go test ./...` и `go vet ./...` проходят. Чтобы удерживать качество при росте проекта, полезно закрепить это в одном локальном сценарии.

Рекомендация:

- Добавить `make test` или `scripts/check.sh` с:
  - `gofmt` check;
  - `go vet ./...`;
  - `go test ./...`;
  - опционально `go test -race ./...` для nightly/CI.
- Не включать тяжелые Docker/Postgres контракты в быстрый default, если они замедляют локальный цикл.

Ожидаемый эффект: единая команда проверки перед PR/commit.

## Предлагаемый порядок внедрения

1. Ввести SQL dialect/helper и перевести один ограниченный модуль, лучше `jobs`.
2. Добавить benchmarks для `ClaimNext` и `RefreshWorkflowStatus`.
3. Разделить `jobs/store.go` и `scanner/store.go` без изменения поведения.
4. Переделать `httpapi.New` на явный router registration contract.
5. После измерений оптимизировать workflow refresh и scan DB writes.

## Проверки, выполненные во время анализа

```text
go test ./...
go vet ./...
```

Обе команды завершились успешно.
