# Архитектура T-Helper

## Назначение

`t-helper` - модульная on-premise система для:

- обнаружения Terraform working directories по файловой системе;
- регистрации проектов и Git-репозиториев в persistent storage;
- локального анализа Terraform-кода без SaaS и без передачи исходников наружу;
- управления конфигурацией, пользователями, доступом и модулями через `GUI`, `Web UI` и `thelper-ctl`.

## Архитектурные принципы

- source of truth для runtime-конфигурации и рабочих сущностей - БД;
- файлы `config.json` и `.t-helper.ignore` используются только для первичного импорта через `thelper-ctl -reconfigure`;
- `GUI` и `Web UI` используют единый backend API и должны быть функционально эквивалентны в рамках сценариев, включённых в конкретный релиз;
- Frontend MVP покрывает основные read/operate сценарии, а полный административный UI для auth/RBAC/SCIM может поставляться на этапе hardening;
- любой модуль должен иметь чёткий lifecycle и поддерживать независимый restart;
- монолитный и распределённый режимы должны использовать совместимую модель взаимодействия;
- Terraform-код, findings и security rules не покидают периметр установки.

## Terraform-проект в MVP

Terraform-проектом считается директория, содержащая хотя бы один файл `*.tf`.

Ограничения MVP:

- директории только с `.terraform`, `*.tfstate`, `*.tfvars` без `*.tf` проектом не считаются;
- после обнаружения Terraform-проекта обход ниже этой директории прекращается;
- поддержка `terragrunt.hcl` рассматривается как расширение следующих версий.

## Логические модули

- `core` - оркестрация, API, lifecycle, журналирование, jobs framework
- `global-scanner` - обход глобальных `root_path` из `scanning.global_scann`, ignore matcher, обнаружение Terraform-проектов и Git-репозиториев
- `project-scanner` - project-level Terraform scan по настройкам отдельного проекта; baseline toolchain: `terraform validate`, `TFLint`
- `security-validator` - security/validation scan по настройкам отдельного проекта и доступным модулям из `scanning.security_scan.modules`; baseline toolchain: `Trivy`, `Gitleaks`, `Checkov`, enterprise-policy checks через `OPA`/`Conftest`
- `repository-manager` - `clone`, `pull`, `sync`, webhook/polling operations, защита от конфликтующих операций
- `config-manager` - импорт, валидация и применение конфигурации из БД
- `auth` - authentication providers, `SCIM`, `RBAC`, users, groups, roles, bindings
- `module-runtime` - состояния модулей, `reload`, `restart`
- `web` - поставка `Web UI`
- `gui` - desktop-клиент для локального использования
- `nginx` - reverse proxy, TLS termination, static delivery

## Сценарии развёртывания

### Монолитный режим

На одном хосте работают:

- `core`
- `global-scanner`
- `project-scanner`
- `security-validator`
- `repository-manager`
- `config-manager`
- `auth`
- `module-runtime`
- `web`
- `nginx`

`GUI` запускается локально на рабочем месте администратора или пользователя.

### Разнесённый режим

Отдельно могут быть вынесены:

- `global-scanner`
- `project-scanner`
- `security-validator`
- `repository-manager`
- `auth`
- `web`
- `nginx`
- БД

## Storage strategy

Целевые хранилища по roadmap:

- `Badger`
- `SQLite`
- `PostgreSQL`
- `MySQL`
- `MongoDB`

Базовые значения:

- `Badger` - default internal storage for local setup
- `SQLite` - optional internal storage
- `PostgreSQL` - default external storage for server setup
- `MySQL` - optional external storage

Требования к слою хранения:

- логическая модель данных едина для всех backends;
- backend использует storage abstraction;
- SQL-хранилища поддерживают миграции;
- внутренняя БД выбирается через `database.database_type` и `database.database_path`;
- если `external_databases.enabled = true`, runtime использует `external_databases` и не использует внутреннюю БД для рабочих данных;
- `Badger` и `PostgreSQL` входят в первый обязательный этап реализации;
- `SQLite`, `MySQL` и `MongoDB` поставляются отдельными adapters на этапе Operational Hardening.

### Repo storage strategy

Для локального размещения клонированных репозиториев используется тот же набор `root_path`, что и для глобального сканирования: `scanning.global_scann`.

Правила:

- при clone пользователь выбирает существующий `root_path` или создаёт новый root path;
- внутри выбранного root path пользователь выбирает существующую target directory или создаёт новую;
- clone workflow использует provider-aware adapters для `gitlab`, `github`, `bitbucket`;
- protocol `https|ssh` выбирается на уровне clone request и влияет на итоговый clone URL;
- для `gitlab` поддерживается отдельный recursive group clone workflow, который перечисляет все проекты группы и всех вложенных subgroup'ов;
- если clone выполняется в новый path, `repository-manager` добавляет его в список `root_paths`;
- локальный путь репозитория строится внутри выбранного `root_path`;
- `full_path` и итоговый `local_path` должны быть нормализованы и не должны позволять выход за пределы выбранного `root_path`;
- если каталог уже содержит корректный Git-репозиторий с ожидаемым remote, `clone` заменяется на `pull`;
- операции `clone`, `pull` и `sync` по одному `repository.id` сериализуются через `job_locks`.

## Базовый runtime flow

1. `thelper-ctl -reconfigure` импортирует конфигурацию и ignore rules в БД.
2. `thelper` поднимает runtime и читает активную конфигурацию из БД.
3. `global-scanner` запускает глобальное сканирование по `root_path` из `scanning.global_scann`.
4. Обнаруженные Terraform-проекты и Git-репозитории регистрируются в storage.
5. `repository-manager` поддерживает `clone`, `pull`, `sync`; при clone в новый path он добавляет этот path в `root_paths`.
6. `project-scanner` выполняет project-level Terraform scan по настройкам конкретного проекта с использованием `terraform validate` и `TFLint`.
7. `security-validator` выполняет security/validation scan по настройкам конкретного проекта с использованием `Trivy`, `Gitleaks`, `Checkov` и при необходимости `OPA`/`Conftest`, затем сохраняет findings.
8. `module-runtime` обеспечивает `reload`, `restart` и статусы модулей.

## Ключевые архитектурные риски, устранённые рефакторингом документации

- убрана зависимость от ссылок на номера строк в другом документе;
- продуктовые требования и технические механики разведены по отдельным документам;
- дубли и несовпадающие формулировки сведены к одному источнику истины;
- репозиторий получил устойчивую структуру для последующего code scaffolding.
