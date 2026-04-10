# T-Helper

`t-helper` - on-premise платформа для обнаружения Terraform-проектов, учёта репозиториев, локального security-анализа и централизованного управления конфигурацией, доступом и модулями.

Репозиторий находится в documentation-first состоянии: требования и технические решения разнесены по каноническим документам, чтобы следующий этап code scaffolding опирался на согласованный контракт.

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
- `thelper-ctl` - административный CLI

## Карта документации

- [`docs/requirements.md`](docs/requirements.md) - функциональные и нефункциональные требования.
- [`docs/architecture.md`](docs/architecture.md) - модули, deployment modes, storage strategy и runtime flow.
- [`docs/interfaces.md`](docs/interfaces.md) - CLI, backend API, конфигурация и алгоритм глобального сканирования.
- [`docs/api.md`](docs/api.md) - HTTP API conventions, response schemas и endpoint skeleton для MVP scaffolding.
- [`docs/configuration.md`](docs/configuration.md) - структура `config.json`, `.t-helper.ignore`, reloadability и валидация.
- [`docs/data-model.md`](docs/data-model.md) - сущности, связи, enum-значения и storage-инварианты.
- [`docs/payload-schemas.md`](docs/payload-schemas.md) - версионируемые JSON payload/result contracts для jobs, scans и module states.
- [`docs/access-control.md`](docs/access-control.md) - auth, SCIM, RBAC, permissions и API authorization matrix.
- [`docs/traceability.md`](docs/traceability.md) - трассировка требований к API, модели данных, permissions, этапам и приёмке.
- [`docs/test-plan.md`](docs/test-plan.md) - mapping MVP acceptance к API, storage, authorization и runtime checks.
- [`docs/roadmap.md`](docs/roadmap.md) - этапы реализации, критерии приёмки и открытые вопросы.
- [`docs/project-structure.md`](docs/project-structure.md) - стартовый каркас репозитория для этапа Foundation и последующих модулей.
- [`config.example.json`](config.example.json) - валидный пример входного `config.json` для `thelper-ctl -reconfigure`.

## Границы MVP

- Foundation: `thelper`, `thelper-ctl`, storage abstraction, `PostgreSQL`, `Badger`, конфигурация, jobs framework и lifecycle модулей.
- Scanner MVP: отдельные модули `global-scanner`, `project-scanner` и `security-validator`, `root_path` из `scanning.global_scann`, ignore rules, обнаружение Terraform-проектов по `*.tf`, обнаружение Git-репозиториев и регистрация объектов в БД.
- Repository Manager MVP: provider-aware `clone`, `pull`, `sync`, размещение репозиториев внутри путей из `scanning.global_scann`, выбор root path и target directory, webhook/polling sync, recursive clone GitLab groups/subgroups и защита от конфликтующих операций.
- Security Validator MVP: локальные security/validation scans по настройкам проекта, стек `terraform validate` + `TFLint` для `project-scanner`, `Trivy` + `Gitleaks` + `Checkov` для `security-validator`, enterprise-policy checks через `OPA`/`Conftest`, rule sets и findings.
- Auth MVP: локальная аутентификация, расширяемая модель external providers, базовые `SCIM`/`RBAC` контракты и audit security-событий. Полный administrative UI для auth/RBAC/SCIM допускается как отдельный hardening-этап.
- Frontend MVP: `Web UI` и локальный `GUI` для основных read/operate сценариев. Backend API должен покрывать сценарии MVP, а расширенные administrative endpoints поставляются по roadmap.

## Базовый стек сканирования

- `global-scanner`: файловое обнаружение Terraform-проектов и Git-репозиториев.
- `project-scanner`: `terraform validate`, `TFLint`, проверки providers/auth/syntax/deprecations/quality/policy.
- `security-validator`: `Trivy`, `Gitleaks`, `Checkov`, findings по misconfiguration и secrets.
- Enterprise-policy layer: `OPA`/`Conftest` как подключаемые policy engines.

## Базовые технические решения

- source of truth для runtime-конфигурации и рабочих данных - БД;
- Terraform-проект в MVP определяется по наличию `*.tf`;
- глобальное сканирование не читает содержимое Terraform-файлов без необходимости;
- `GUI` и `Web UI` используют единый backend API;
- `GUI` работает только локально;
- findings и исходный код не отправляются во внешние сервисы.

## Стартовый каркас репозитория

В репозитории создан базовый layout для начала реализации:

- `cmd/` - точки входа `thelper` и `thelper-ctl`;
- `internal/app/` - assembly/runtime wiring для API и CLI;
- `internal/modules/` - изолированные runtime-модули по архитектурной декомпозиции;
- `internal/domain/` - доменные сущности и use case boundaries;
- `internal/platform/` - storage, jobs, logging, HTTP, locking и runtime infrastructure;
- `internal/contracts/` - API DTO и versioned payload contracts;
- `web/` и `ui/` - заготовки под `Web UI` и локальный `GUI`;
- `deploy/` - инфраструктурные артефакты доставки;
- `configs/`, `scripts/`, `test/`, `build/` - конфигурация, служебные скрипты, тесты и build assets.

Подробное описание структуры и её связи с документацией вынесено в [`docs/project-structure.md`](docs/project-structure.md).
