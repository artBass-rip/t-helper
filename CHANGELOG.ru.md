# Журнал изменений

Здесь фиксируются заметные изменения репозитория.

У проекта пока нет semantic version tags. Исторические записи сгруппированы по
завершенным roadmap stages и merge commits.

## Не выпущено

- Проведен аудит документации репозитория относительно текущего code baseline;
  исправлены development/local-environment документы, которые всё ещё
  описывали Stage 04 как текущий этап.
- Development docs обновлены описанием Stage 05 migration versions, repository
  manager API behavior, path/credential validation и worker-side выполнения
  clone/pull/sync.
- Реализован Stage 05 repository manager MVP: выбран managed provider GitHub,
  добавлены generic Git, provider profiles, masked credentials, enqueue API для
  clone/pull/sync и worker handlers.
- Усилен Stage 05 repository manager: добавлена строгая ADR 0016 path parsing
  validation, machine-readable коды repository validation, более строгая
  проверка credential usages, worker-side разрешение `secretref://env/...`,
  clone idempotent replay до pre-create reservations и worker-side проверка
  статуса/пути repository.
- Добавлен hardening Stage 05: проверка совместимости credential protocol,
  non-interactive Git execution, redaction ошибок repository operations и
  cleanup operation reservations.
- Уточнен `docs/api.md`: теперь он явно разделяет executable API baseline и
  future-stage endpoint contracts.
- Обновлены карты документации в README: API-документ описан как executable
  baseline и roadmap contract surface.
- Добавлены двуязычные GitHub Pages в `docs/index.html` и `docs/en.html` с
  видимой пометкой в начале каждой страницы, что проект реализуется
  исключительно ИИ.
- Добавлен deployment GitHub Pages через `.github/workflows/pages.yml` из
  директории `docs`.
- Pages workflow переведен на публикацию директории `docs` в ветку `gh-pages`
  вместо GitHub Pages REST deployment API.
- Добавлена документация GitHub Pages и ссылки на неё из обеих версий README.
- GitHub Pages переведены на темную framed layout с sticky header для
  документации, responsive product overview hero и общей темной HTML-оболочкой
  документации.
- Development, local environment и test-plan документация обновлены под
  executable baseline.
- Следующий плановый этап реализации: Stage 06 project scanner/security
  validator MVP.
- Stage 09 implementation spec и roadmap теперь фиксируют приоритизированный
  runtime/scanner optimization backlog из code review.
- Добавлен русский журнал изменений: [`CHANGELOG.ru.md`](CHANGELOG.ru.md).
- Английский README сделан основным файлом [`README.md`](README.md).
- Добавлен русский README: [`README.ru.md`](README.ru.md).
- README расширены описанием Stage 05 status, executable commands, HTTP API
  surface, configuration model, storage/migration scope, scanner behavior,
  documentation map, roadmap status и security/privacy assumptions.

## Stage 04: Scanner and Registry MVP - 2026-05-26

Коммиты: `490da8e` - `274dcee`.

### Добавлено

- Добавлены Stage 04 миграции SQLite/PostgreSQL для `root_paths`, `projects`,
  `project_links`, минимальных generic `repositories`, `environments` и
  `workspaces`.
- Добавлены scanner/registry API endpoints для root paths, scans, projects,
  project links, repositories, ignore rules, environments и workspaces.
- Добавлена обработка `global_scan` jobs, обнаруживающая Terraform working
  directories только по именам файлов `*.tf`.
- Добавлены coalesced background `project_discovery` jobs для определения Git
  repository после регистрации проектов глобальным scan.
- Добавлена обработка Git markers: директории `.git/` и файлы `.git` с
  `gitdir:`.
- Добавлены generic local repository cards и связи `same_repository` для
  отдельных проектов, найденных внутри одного repository.
- Добавлены filesystem boundary tests и scanner filesystem abstraction.

### Изменено

- Global scanner пропускает symlinked directories и `.git/`, прекращает обход
  ниже найденного Terraform project и считает skipped symlink/error counters.
- Root paths теперь хранят source, а inactive discovery paths не сканируются.
- Ignore matching в Stage 04 работает как exclude-only, сохраняя `!pattern`
  rules без применения negation до реализации full `.gitignore` semantics.
- Список проектов по умолчанию возвращает active projects; missing, disabled и
  all records выбираются явными фильтрами.
- API-документация актуализирована для mixed root scans, project links и Stage
  04 scanner behavior.

### Ограничения

- `POST /api/project-scans` реализует только Stage 06 lifecycle guard и для
  валидных active projects возвращает `project_scan_unavailable`.
- Repository clone, pull и sync workflows запланированы на Stage 05.
- Full `.gitignore` negation semantics запланирована на один из hardening
  stages.

## Stage 03: Jobs, Workers and Status Foundation - 2026-05-21

Merge commit: `7732215` (`stage-03-jobs-workers-status`).

### Добавлено

- Добавлены Stage 03 миграции SQLite/PostgreSQL для `jobs`, `job_locks`,
  `job_events` и `workflow_statuses`.
- Добавлены persistent job enqueue, atomic claim, leases, heartbeat,
  retry/backoff и expired lease recovery.
- Добавлено выполнение queued background jobs через `thelper-worker`.
- Добавлены worker handlers для `config_reload` и `module_restart`.
- Добавлена идемпотентность jobs в scope actor, job type и `Idempotency-Key`.
- Добавлены `/api/jobs`, `/api/jobs/{id}` и `/api/status*` read APIs.
- Добавлены runtime, workflow, job и worker status read models.

### Изменено

- Job idempotency сравнивает canonical JSON payloads, поэтому эквивалентные
  payloads с разным порядком полей или whitespace переиспользуют тот же job.
- Worker status показывает каждый running lease, включая concurrent jobs одного
  worker process.
- SQLite workers соблюдают настроенный лимит одного process через
  database-fingerprint worker lock.
- Retention cleanup сохраняет events для active jobs и удаляет только старые
  routine events terminal jobs.
- Job enqueue отклоняет secret-like payloads до сохранения.

## Stage 02 Hardening and Documentation Alignment - 2026-05-06

Коммит: `0b5996e` (`Stage02: preserve ignore_rules and sync reload`).

### Исправлено

- Исправлена семантика `PUT /api/config`: HTTP config import сохраняет
  существующие imported system `ignore_rules`; `.t-helper.ignore` остается зоной
  ответственности `thelper-ctl -reconfigure` и будущих ignore-rules API.
- Исправлена обработка `POST /api/modules/reload`: malformed JSON возвращает
  `validation_error`, а не молча превращается в reload всех ключей.
- Исправлена валидация `POST /api/modules/restart`: пустой `module_name`
  возвращает `validation_error`.
- Исправлен config reload: reload с `modules.enabled` заново seed'ит persisted
  `module_states` из active runtime config.
- Исправлен `thelper-ctl -reload`: CLI использует тот же путь обновления
  persisted module state, что и HTTP reload.

### Добавлено

- Добавлены tests на сохранение imported `ignore_rules` при config-only import.
- Добавлены HTTP tests для malformed module reload JSON и отсутствующего
  restart `module_name`.
- Добавлены Stage 02 synchronous result schemas:
  `config_import.result.v1`, `config_reload.result.v1`,
  `module_reload.result.v1`, `module_restart.result.v1` и
  `storage_migration.result.v1`.

### Изменено

- README, roadmap, development docs и local environment docs синхронизированы с
  завершенным Stage 02 executable baseline.
- Уточнено, что Stage 02 config/module lifecycle endpoints являются
  synchronous и не создают Stage 03 `jobs`.
- Уточнено, что Stage 02 владеет imported system `ignore_rules`, а Stage 04
  владеет scanner/API behavior для root-path и project ignore rules.
- API, traceability, data model, test plan и Stage 02 implementation spec
  приведены к code-level Stage 02 contract.

## Stage 02: Config, Modules and Runtime Lifecycle - 2026-05-06

Merge commit: `39446dd` (`stage-02-config-modules-runtime`).

### Добавлено

- Добавлены Stage 02 storage migrations для SQLite и PostgreSQL.
- Добавлена persisted runtime configuration через `config_entries`.
- Добавлено хранение storage profiles через `storage_profiles` и
  `storage_provider_settings`.
- Добавлена persisted module observability через `module_states`.
- Добавлена поддержка import `ignore_rules` при reconfiguration.
- Добавлен строгий pipeline import и validation конфигурации.
- Добавлены команды `thelper-ctl`: `reconfigure`, `reload`, `restart` и
  `migrate-db`.
- Добавлены HTTP endpoints для config и module lifecycle operations.
- Добавлен initial module registry seed для core MVP modules.
- Добавлены singleton runtime lock и health metadata handling.
- Добавлены Stage 02 end-to-end tests для config import, reload, module
  lifecycle, runtime startup, storage profile migration и singleton runtime
  behavior.

### Изменено

- Runtime startup читает active storage/runtime configuration из БД, а не
  считает file configuration source of truth.
- Configuration validation отклоняет unknown top-level и nested keys.
- Deprecated configuration aliases отклоняются, а не принимаются как alternate
  spellings.
- Global scan roots принимаются только через `scanning.global_scan`.
- `GET /api/config` маскирует sensitive values и не раскрывает resolved
  secrets.
- Storage target changes сначала обновляют `migration` profile; active
  `current` profile меняется только после успешного `thelper-ctl migrate-db`.
- Module reload/restart operations обновляют persisted module state.
- `GET /api/health` сохраняет Stage 01 response shape `health_status.v1`,
  добавляя singleton runtime lock/probe semantics.

### Исправлено

- Storage profile imports сохраняют active `current` storage до успешного
  `thelper-ctl -migrate-db`.
- Повторные storage migrations используют fingerprint-specific migration
  profile IDs и retire previous migration targets до подготовки нового.
- Re-import config в уже promoted storage target сохраняет active profile
  identity.
- `repositories.poll_interval_default` отклоняет zero и negative durations.
- Reload results различают accepted keys и реально applied keys.
- Module lifecycle failures сохраняют `state = failed` с `last_error`.
- Runtime startup fails closed при неожиданных ошибках чтения `current` storage
  profile вместо silent fallback на bootstrap storage.
- PostgreSQL storage profile DSNs экранируют credentials и используют
  `net.JoinHostPort` для IPv6-safe host formatting.
- Module reload/restart и `PUT /api/config` отклоняют unknown fields, trailing
  payloads и некорректные `null` requests.

### Безопасность

- Literal sensitive values для external database credentials отклоняются.
- Secret references сохраняются как references и не persist/return как
  plaintext secrets.
- Runtime lock metadata использует safe database fingerprints вместо raw DSNs
  или filesystem paths.

### Ограничения

- Stage 02 использует synchronous lifecycle operations и не создает Stage 03
  jobs.
- Scanner, repository manager, security validator, auth и frontend behavior
  остаются future-stage deliverables.
- MySQL и MSSQL принимаются в forward-compatible configuration spelling, но
  runtime migration сейчас реализован только для SQLite и PostgreSQL.

## Stage 01: Backend Storage Foundation - 2026-05-05

Merge commit: `eec0256` (`stage-01-backend-storage-foundation`).

### Добавлено

- Добавлен Go module `github.com/artBass-rip/t-helper`.
- Добавлены executable entrypoints: `cmd/thelper`, `cmd/thelper-worker` и
  `cmd/thelper-ctl`.
- Добавлен backend HTTP runtime scaffold на `net/http` и `chi`.
- Добавлен safe unauthenticated `GET /api/health` endpoint с
  `health_status.v1`.
- Добавлен correlation ID middleware.
- Добавлены storage abstraction и provider registry.
- Добавлены SQLite и PostgreSQL storage providers.
- Добавлена нормализация external provider name `postgresql` во internal
  `postgres`.
- Добавлен dialect-specific migration runner на `goose`.
- Добавлены Stage 01 SQLite/PostgreSQL migrations для `system_metadata`.
- Добавлены storage fingerprints для safe health metadata.
- Добавлена диагностика `thelper-ctl providers`.
- Добавлен buildable scaffold `thelper-worker`.
- Добавлен GitHub Actions CI для formatting, tests и entrypoint builds.
- Добавлены tests для storage registry behavior, migrations, storage contracts,
  health response shape и server startup.

### Изменено

- Репозиторий перешел от documentation-only planning к executable backend
  scaffold.
- Зафиксирована stage-owned migration policy: Stage 01 создает только Stage
  01-owned system tables и не pre-create later-stage product tables.
- README, development docs, roadmap, test plan и implementation specs
  обновлены под Stage 01 executable baseline.

### Ограничения

- Worker job execution на Stage 01 является scaffold-only и начинается в Stage
  03.
- Runtime configuration, module lifecycle и singleton lock enforcement являются
  Stage 02 responsibilities.
- Product/domain behavior для scanner, repository manager, auth и frontend
  зарезервированы для следующих roadmap stages.

## Stage 00: Delivery Contract - 2026-05-05

Merge commit: `cf9474d` (`stage-00-delivery-contract`).

### Добавлено

- Добавлены Stage 00 delivery contract и decision register.
- Добавлены repository-level implementation rules, Stage 01 scaffolding
  checklist и Stage 01-03 backlog.
- Добавлены Docker Compose files для development, manual checks, OS matrix
  checks и test execution.
- Добавлены placeholders структуры `.artifacts` для local environment, logs,
  repositories, SQLite data и test outputs.
- Добавлен пример local environment file.
- Добавлены implementation-spec documentation index updates.
- Добавлены `.gitignore` entries для generated artifacts и local runtime files.

### Изменено

- Уточнено, что Stage 00 поставляет только documentation и decisions.
- Executable scaffolding, CI workflow files и storage test harnesses отложены
  до Stage 01.

## Initial Documentation Baseline - 2026-04-09 to 2026-05-05

### Добавлено

- Добавлены initial project README и configuration example.
- Добавлена core product documentation для requirements, architecture,
  interfaces, API, configuration, data model, payload schemas, access control,
  traceability, test plan, roadmap и technology stack.
- Добавлены ADRs по backend stack, storage and migrations, job workers,
  frontend/Tauri runtime policy, configuration compatibility, secret
  resolution, singleton runtime health, repository identity/provider parsing,
  local password hashing, security finding fingerprints и external toolchain
  profiles.
- Добавлены implementation specs для planned stages.

### Состояние репозитория

- Текущий implemented backend scope: Stage 05.
- Текущие supported storage adapters: SQLite и PostgreSQL.
- Текущая public runtime API surface: health, config, module lifecycle,
  jobs/status, scanner/registry и repository manager.
- Frontend directory существует как placeholder; React/Tauri implementation
  начинается в Stage 08.
