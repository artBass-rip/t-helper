# T-Helper

`t-helper` - on-premise платформа для обнаружения Terraform-проектов, учёта репозиториев, локального security-анализа и централизованного управления конфигурацией, доступом и модулями.

Репозиторий перешёл из documentation-only состояния в Stage 01 executable
scaffold: backend entrypoints, storage foundation, migrations, health endpoint,
tests and CI are implemented. Product/domain behavior for config, jobs,
scanner, repository manager, auth and frontend remains stage-owned by later
roadmap stages.

## License

Internal proprietary project. All rights reserved.

Use, copying, modification, distribution, and access outside the authorized organization or team are prohibited without prior written permission from the copyright holder.

`t-helper` - on-premise система для:

- обнаружения Terraform-проектов в файловой системе;
- регистрации проектов и репозиториев в БД;
- локального security-анализа Terraform-кода без SaaS;
- управления конфигурацией, модулями и доступом через `GUI`, `Web UI` и `thelper-ctl`.

## Исполняемые компоненты

- `thelper` - backend runtime и service process
- `thelper-worker` - отдельный worker process для выполнения background jobs
- `thelper-ctl` - административный CLI

Stage 01 реализует минимальные executable skeletons. `thelper` starts the HTTP
runtime, applies Stage 01 migrations and exposes `GET /api/health`.
`thelper-worker` is a buildable scaffold; job execution starts in Stage 03.
`thelper-ctl` currently includes Stage 01 provider diagnostics, while
configuration/lifecycle commands start in Stage 02.

## Карта документации

- [`docs/requirements.md`](docs/requirements.md) - функциональные и нефункциональные требования.
- [`docs/architecture.md`](docs/architecture.md) - модули, deployment modes, storage strategy и runtime flow.
- [`docs/interfaces.md`](docs/interfaces.md) - CLI, backend API, конфигурация и алгоритм глобального сканирования.
- [`docs/api.md`](docs/api.md) - HTTP API conventions, response schemas и endpoint skeleton для MVP scaffolding.
- [`docs/configuration.md`](docs/configuration.md) - структура `config.json`, `.t-helper.ignore`, reloadability и валидация.
- [`docs/technology-stack.md`](docs/technology-stack.md) - зафиксированный backend, frontend и desktop GUI stack.
- [`docs/frontend-ui-contract.md`](docs/frontend-ui-contract.md) - route map, navigation, density и Tauri delivery policy для Stage 08.
- [`docs/adr/`](docs/adr/) - принятые implementation-level architecture decisions.
- [`docs/development.md`](docs/development.md) - локальный dev/test contract для scaffolding, dialect-specific migrations, SQLite/PostgreSQL и secret references.
- [`docs/local-dev-environment.md`](docs/local-dev-environment.md) - Docker-based локальное окружение разработчика, product containers, dependency services, OS matrix, automated/manual testing.
- [`docs/data-model.md`](docs/data-model.md) - сущности, связи, enum-значения и storage-инварианты.
- [`docs/payload-schemas.md`](docs/payload-schemas.md) - версионируемые JSON payload/result contracts для jobs, scans и module states.
- [`docs/access-control.md`](docs/access-control.md) - auth, SCIM, RBAC, permissions и API authorization matrix.
- [`docs/traceability.md`](docs/traceability.md) - трассировка требований к API, модели данных, permissions, этапам и приёмке.
- [`docs/test-plan.md`](docs/test-plan.md) - mapping MVP acceptance к API, storage, authorization и runtime checks.
- [`docs/roadmap.md`](docs/roadmap.md) - этапы реализации, критерии приёмки и статусы открытых решений.
- [`docs/stage-00-delivery-contract.md`](docs/stage-00-delivery-contract.md) - принятый Stage 00 delivery contract, Stage 01 checklist и backlog Stage 01-03.
- [`config.example.json`](config.example.json) - валидный пример входного `config.json` для `thelper-ctl -reconfigure`.

## Текущий executable baseline

- Go module: `github.com/artBass-rip/t-helper`, Go `1.23`.
- Entrypoints: `cmd/thelper`, `cmd/thelper-worker`, `cmd/thelper-ctl`.
- HTTP: `net/http` + `chi`, correlation IDs, unauthenticated safe
  `GET /api/health` returning `health_status.v1`.
- Storage: pluggable registry with `sqlite` and `postgres` MVP adapters;
  external provider name `postgresql` is normalized to internal `postgres`.
- Migrations: `goose` runner with dialect-specific synchronized migrations
  under `internal/storage/migrations/{sqlite,postgres}`; `mysql` and `mssql`
  directories are reserved for Stage 10.
- Stage 01 schema ownership: only `system_metadata` plus migration metadata.
  Later-stage tables are intentionally not created.
- CI: GitHub Actions `ci / go` runs format check, `go test ./...` with
  PostgreSQL service and build checks for all three entrypoints.

Recommended local checks:

```text
go test ./...
go build ./cmd/thelper ./cmd/thelper-worker ./cmd/thelper-ctl
docker compose --profile offline -f docker-compose.test.yml run --rm test-runner
```

## Технологический стек

- Backend: `Go`.
- Frontend: `React`, `TypeScript`, `Vite`, `TanStack Router`, `TanStack Query`, `Zod`, `React Hook Form`, `Ant Design`.
- Desktop GUI: `Tauri` поверх той же React/TypeScript codebase.
- Storage MVP: `SQLite` для локального режима и `PostgreSQL` для server setup.
- Storage platform targets: `SQLite`, `PostgreSQL`, `MySQL`, `MSSQL`.
- Jobs: отдельные `thelper-worker` процессы.

## Stage внедрения MVP

- Stage 00: delivery contract, Definition of Done и закрытие открытых продуктовых решений.
- Stage 01: completed backend skeleton, `thelper`, `thelper-worker`, `thelper-ctl`, storage abstraction, `SQLite`, `PostgreSQL`, migrations и HTTP skeleton.
- Stage 02: runtime configuration, `config_entries`, module lifecycle, reload/restart и singleton runtime policy.
- Stage 03: jobs framework, worker execution model, leases, `job_locks`, `job_events` и базовый `status-monitor`.
- Stage 04: `global-scanner`, `root_paths`, ignore rules, Terraform project discovery, background `project_discovery`, registry `projects`/`project_links`/`repositories`, `environments`/`workspaces` backend API.
- Stage 05: repository manager MVP, generic Git + GitLab/GitHub single-repository `clone`/`pull`/`sync`, target path safety, credentials и serialization через `job_locks`.
- Stage 06A: full external toolchain profile runtime, registry, validator, certified profiles and optional analyzer for `terraform validate`, `TFLint` and `Trivy`.
- Stage 06B: project/security scans MVP, `terraform validate`, `TFLint`, `Trivy` как обязательный MVP security scanner, rule set registry, findings и parent-child scan workflow.
- Stage 07: auth, локальная аутентификация, базовые external provider contracts, `RBAC`, SCIM contract/stub и audit security-событий.
- Stage 08: `Web UI` и локальный `GUI` на общей React/TypeScript codebase для основных read/operate сценариев и full MVP administrative screens.

Platform hardening поставляется отдельными stages: runtime/observability hardening, storage expansion, security tooling, admin UI hardening/extensions, SCIM full sync и repository webhooks. Distributed deployment поставляется в Stage 15.

## Базовый стек сканирования

- `global-scanner`: файловое обнаружение Terraform-проектов и enqueue фоновых `project_discovery` jobs.
- `project-scanner`: `terraform validate`, `TFLint`, проверки providers/auth/syntax/deprecations/quality/policy.
- `security-validator`: `Trivy` как обязательный MVP scanner, findings по misconfiguration и secrets.
- Security/policy extensions: `Gitleaks`, `Checkov`, `OPA`/`Conftest`.

## Базовые технические решения

- source of truth для runtime-конфигурации и рабочих данных - БД;
- Terraform-проект в MVP определяется по наличию `*.tf`;
- глобальное сканирование не читает содержимое Terraform-файлов без необходимости;
- `GUI` и `Web UI` используют единый backend API;
- `GUI` работает только локально;
- в одной локальной установке активен только один `t-helper` runtime: если `thelper` уже запущен, Tauri подключается к нему; если нет - Tauri запускает его, а `Web UI` подключается к тому же runtime;
- findings и исходный код не отправляются во внешние сервисы.
