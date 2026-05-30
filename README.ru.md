# T-Helper

[English version](README.en.md)

`t-helper` - on-premise платформа для обнаружения Terraform-проектов, учета
метаданных репозиториев, локального security-анализа и централизованного
управления runtime-конфигурацией, модулями, jobs и доступом.

Текущий репозиторий находится на **Stage 05 repository manager MVP baseline**.
Реализована executable backend foundation: entrypoints сервисов, storage
adapters, миграции, health checks, persisted runtime configuration, module
lifecycle, singleton runtime lock, jobs/workers/status, global scanning,
Terraform project discovery, root paths, project registry, scanner/registry
HTTP APIs и repository manager clone/pull/sync workflows.

Project/security scans, authentication/RBAC и frontend намеренно отнесены к
следующим roadmap stages.

## Страница проекта

GitHub Pages поставляются как статическая двуязычная landing page проекта с
генерируемыми двуязычными страницами документации:

- русская страница: [docs/index.html](docs/index.html);
- английская страница: [docs/en.html](docs/en.html);
- deployment workflow: [.github/workflows/pages.yml](.github/workflows/pages.yml).

Страница использует текущую темную framed Pages layout: sticky navigation,
product overview hero, знак T-Helper, feature strip и темную HTML-оболочку
документации. В начале страницы явно указано, что проект реализуется
исключительно ИИ. Русские Markdown-источники находятся в `docs/ru/`;
соответствующие английские Markdown-источники находятся в `docs/en/` с теми же
относительными путями. Во время публикации `docs/build-pages.js` рендерит эти
парные источники в русскую и английскую HTML-оболочку с локальной навигацией,
связанными документами и ссылками внутри той же языковой версии. Публикация
идет из директории `docs` через GitHub Actions в ветку `gh-pages`. В настройках
Pages нужно выбрать source `Deploy from a branch`, branch `gh-pages`, folder
`/ (root)`.

## Лицензия

Internal proprietary project. All rights reserved.

Use, copying, modification, distribution, and access outside the authorized
organization or team are prohibited without prior written permission from the
copyright holder.

## Что реализовано

- Обнаружение Terraform working directories по именам файлов `*.tf`.
- Persisted registry для root paths, projects, project links, минимальных
  repositories, environments и workspaces.
- Runtime-конфигурация хранится в БД и импортируется через `thelper-ctl`.
- Состояние модулей, reload и restart operations.
- Background job queue с persistent leases, heartbeat, retry/backoff,
  `job_locks`, job events и status read models.
- `global_scan` jobs, которые ставят coalesced `project_discovery` jobs для
  определения Git-связей без блокировки результата global scan.
- Stage 05 repository manager APIs для provider profiles, masked credentials,
  safe repository identity normalization, clone/pull/sync job enqueueing и
  worker execution.
- SQLite и PostgreSQL storage adapters для текущего MVP baseline.
- HTTP APIs для health, config, modules, jobs/status, scanner/registry и
  repository management.

## Исполняемые компоненты

- `thelper` - backend runtime и HTTP service process.
- `thelper-worker` - worker process для queued background jobs.
- `thelper-ctl` - административный CLI для storage diagnostics, import
  конфигурации, reload, restart модулей и контролируемой миграции БД.

Основные команды:

```text
go run ./cmd/thelper -listen 127.0.0.1:8080 -storage-provider sqlite -storage-dsn .artifacts/dev/sqlite/t-helper.db
go run ./cmd/thelper-worker -storage-provider sqlite -storage-dsn .artifacts/dev/sqlite/t-helper.db -concurrency 1
go run ./cmd/thelper-ctl -storage-provider sqlite -storage-dsn .artifacts/dev/sqlite/t-helper.db providers
go run ./cmd/thelper-ctl -config config.example.json -ignore .t-helper.ignore -reconfigure
go run ./cmd/thelper-ctl -reload
go run ./cmd/thelper-ctl -restart global-scanner
go run ./cmd/thelper-ctl -migrate-db
```

При использовании собранных бинарников замените `go run ./cmd/<name>` на имя
бинарника.

## HTTP API

Stage 05 предоставляет следующую runtime API surface:

- `GET /api/health`
- `GET /api/config`, `PUT /api/config`
- `GET /api/modules`, `POST /api/modules/reload`,
  `POST /api/modules/restart`
- `GET /api/jobs`, `GET /api/jobs/{id}`
- `GET /api/status`, `GET /api/status/workflows`,
  `GET /api/status/workflows/{job_group_id}`, `GET /api/status/jobs/{job_id}`,
  `GET /api/status/workers`
- `GET /api/root-paths`, `PUT /api/root-paths`
- `POST /api/scans`, `GET /api/scans/{job_id}`
- `GET /api/projects`, `GET /api/projects/{id}`,
  `GET /api/projects/{id}/links`
- `POST /api/project-scans` lifecycle guard для будущих Stage 06 scans
- `GET /api/repos`, `GET /api/repos/{id}`
- `GET /api/repo-provider-instances`, `PUT /api/repo-provider-instances`
- `GET /api/repo-credentials`, `PUT /api/repo-credentials`
- `POST /api/repos/clone`, `POST /api/repos/pull`,
  `POST /api/repos/sync`
- `GET /api/ignore-rules`, `PUT /api/ignore-rules`
- `GET /api/environments`, `GET /api/environments/{id}`
- `GET /api/workspaces`, `GET /api/workspaces/{id}`

Канонические contracts описаны в [docs/ru/api.md](docs/ru/api.md) и
[docs/ru/interfaces.md](docs/ru/interfaces.md).

## Модель конфигурации

После первичного импорта БД является source of truth для runtime-конфигурации
и рабочих данных.

Файлы импорта:

- `config.json`
- `.t-helper.ignore`

[config.example.json](config.example.json) - валидный reference input для
`thelper-ctl -reconfigure`.

Важное поведение:

- `thelper-ctl -reconfigure` строго валидирует config, импортирует config и
  ignore rules в БД, но не запускает и не перезапускает сервис.
- `thelper-ctl -reload` применяет reloadable settings из БД и сообщает о
  settings, которым нужен restart.
- `thelper-ctl -restart <module>` перезапускает один модуль и обновляет
  `module_states`.
- `external_databases` подготавливает migration target. Runtime storage
  переключается только после успешного `thelper-ctl -migrate-db`.
- Raw secrets отклоняются. Для секретов используйте ссылки `secretref://...`.

## Storage и миграции

- Go module: `github.com/artBass-rip/t-helper`, Go `1.23`.
- Текущие storage providers: `sqlite`, `postgres`.
- External provider name `postgresql` нормализуется во internal `postgres`.
- Миграции dialect-specific и синхронизированы по logical version в
  `internal/storage/migrations/{sqlite,postgres}`.
- Stage 01 schema: `system_metadata` plus migration metadata.
- Stage 02 schema: `config_entries`, `storage_profiles`,
  `storage_provider_settings`, `module_states`, imported system
  `ignore_rules`.
- Stage 03 schema: `jobs`, `job_locks`, `job_events`,
  `workflow_statuses`.
- Stage 04 schema: `root_paths`, `projects`, `project_links`, minimal
  `repositories`, `environments`, `workspaces`.
- Stage 05 schema: provider instances, repository credentials, repository
  operation reservations/indexes and repository manager hardening.
- MySQL и MSSQL являются roadmap targets для Stage 10, а не текущими runtime
  adapters.

## Поведение scanner

- Global scanner обнаруживает Terraform projects по именам файлов `*.tf`.
- Scanner не парсит Terraform source contents на этапе discovery.
- Symlinked directories и `.git/` directories пропускаются в Stage 04.
- Обход прекращается ниже найденного Terraform project.
- Stage 04 ignore matching работает как exclude-only. Правила `!pattern`
  сохраняются, но не применяются до реализации full `.gitignore` semantics в
  одном из следующих hardening stages.
- Missing projects отслеживаются без merge отдельных project rows.
- Проекты из одного Git repository связываются через project links типа
  `same_repository`.

## Локальная разработка

Рекомендуемые проверки:

```text
make test
go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl
docker compose --profile offline -f docker-compose.test.yml run --rm test-runner
```

PostgreSQL contract tests запускаются, когда задан `THELPER_POSTGRES_DSN`.
SQLite tests запускаются по умолчанию.
Для ручных/nightly проверок с race detector используйте `make race`.

## Документация

- [docs/ru/requirements.md](docs/ru/requirements.md) - functional и non-functional
  requirements.
- [docs/ru/architecture.md](docs/ru/architecture.md) - architecture, modules,
  deployment modes и runtime flow.
- [docs/ru/interfaces.md](docs/ru/interfaces.md) - CLI, backend API, configuration и
  global scanning behavior.
- [docs/ru/api.md](docs/ru/api.md) - текущий Stage 05 HTTP API baseline, будущие
  endpoint contracts и response schemas.
- [docs/ru/configuration.md](docs/ru/configuration.md) - `config.json`,
  `.t-helper.ignore`, reloadability и validation.
- [docs/ru/development.md](docs/ru/development.md) - local development и test
  contract.
- [docs/ru/code-optimization.md](docs/ru/code-optimization.md) - выполненные
  оптимизации, quality gate и дальнейший optimization backlog.
- [docs/ru/github-pages.md](docs/ru/github-pages.md) - структура двуязычных GitHub
  Pages и deployment workflow.
- [docs/ru/local-dev-environment.md](docs/ru/local-dev-environment.md) -
  Docker-based local environment.
- [docs/ru/data-model.md](docs/ru/data-model.md) - entities, relationships и storage
  invariants.
- [docs/ru/payload-schemas.md](docs/ru/payload-schemas.md) - versioned JSON
  payload/result contracts.
- [docs/ru/access-control.md](docs/ru/access-control.md) - auth, SCIM, RBAC и
  authorization matrix.
- [docs/ru/frontend-ui-contract.md](docs/ru/frontend-ui-contract.md) - Stage 08 Web
  UI и local GUI contract.
- [docs/ru/roadmap.md](docs/ru/roadmap.md) - implementation stages и acceptance
  criteria.
- [docs/ru/adr/](docs/ru/adr/) - architecture decision records.
- [docs/ru/implementation-specs/](docs/ru/implementation-specs/) - stage-level
  implementation specs.
- [CHANGELOG.md](CHANGELOG.md) и [CHANGELOG.ru.md](CHANGELOG.ru.md) - история
  изменений.

## Roadmap status

- Stage 00: completed delivery contract.
- Stage 01: completed backend/storage foundation.
- Stage 02: completed persisted config, module lifecycle и singleton runtime.
- Stage 03: completed jobs, workers и status foundation.
- Stage 04: completed scanner и registry MVP.
- Stage 05: completed repository manager MVP: generic Git plus GitHub,
  clone/pull/sync jobs, provider profiles, masked credentials, path safety,
  repository enrichment и operation serialization.
- Stage 06A/06B: external toolchain profiles, project scanner и security
  validator MVP.
- Stage 07: auth, RBAC, SCIM contract/stub и audit.
- Stage 08: React/TypeScript Web UI и local Tauri GUI.
- Stage 09-15: observability hardening, storage adapter expansion, security
  policy packs, admin UI hardening, SCIM full sync, repository webhooks и
  distributed deployment.

## Security и privacy assumptions

- Runtime-конфигурация и рабочие данные используют БД как source of truth.
- `GET /api/health` безопасен для unauthenticated local discovery и не должен
  раскрывать secrets, DSNs, users, object-scoped details или filesystem paths.
- Secret values представлены ссылками `secretref://...` и не должны
  сохраняться или возвращаться как plaintext.
- Global scan не отправляет source code во внешние сервисы.
- Planned local security stack не отправляет findings и source code во внешние
  SaaS services.
