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
- `GUI` и `Web UI` используют единый backend API; feature parity требуется только для read/operate сценариев, включённых в конкретный релиз;
- Frontend MVP покрывает основные read/operate сценарии и full MVP administrative screens; Stage 12 отвечает за hardening and platform-only administrative extensions where in release scope;
- любой модуль должен иметь чёткий lifecycle и поддерживать независимый restart;
- все внешние providers, включая database/storage и auth providers, реализуются как отдельные подключаемые модули или библиотеки за стабильными internal interfaces;
- монолитный и распределённый режимы должны использовать совместимую модель взаимодействия;
- Terraform-код, findings и security rules не покидают периметр установки.

## Технологический стек

Канонический стек реализации описан в [`technology-stack.md`](technology-stack.md).

- Backend runtime, CLI, API, module lifecycle, jobs framework и storage adapters реализуются на `Go`.
- `Web UI` реализуется на `React`, `TypeScript`, `Vite`, `TanStack Router`, `TanStack Query`, `Zod`, `React Hook Form` и `Ant Design`.
- Локальный `GUI` реализуется на `Tauri` и использует ту же React/TypeScript codebase, что и `Web UI`.
- Stage 08 route map, navigation model, operational density and Tauri delivery policy are defined in [`frontend-ui-contract.md`](frontend-ui-contract.md).

## Terraform-проект в MVP

Terraform-проектом считается директория, содержащая хотя бы один файл `*.tf`.

Ограничения MVP:

- директории только с `.terraform`, `*.tfstate`, `*.tfvars` без `*.tf` проектом не считаются;
- после обнаружения Terraform-проекта обход ниже этой директории прекращается;
- поддержка `terragrunt.hcl` рассматривается как расширение следующих версий.

## Project discovery и Git marker allowlist в MVP

`global-scanner` в blocking traversal path обнаруживает только Terraform working directories и регистрирует отдельные `projects` rows. Для каждого созданного или обновлённого проекта он ставит фоновый `project_discovery` job и продолжает scan без ожидания результата.

`project_discovery` считает директорию Git working tree только при наличии одного из разрешённых markers:

- `.git/` directory;
- `.git` regular file для worktree/submodule, если первая непустая строка начинается с `gitdir:`.

Файлы и директории `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` и другие похожие convention/config files не являются Git markers в MVP.

`global-scanner` не читает `.git` metadata и не выполняет shell-out в `git`. `project_discovery` может читать `.git` file только как Git metadata, не Terraform source. MVP ограничивает размер читаемого `.git` file до `4 KiB` и не обязан разрешать путь после `gitdir:`.

Если несколько локальных Terraform projects относятся к одному Git repository, project records не merge'ятся. Они остаются отдельными `projects` rows и связываются через общий `repository_id` и `project_links.link_type = same_repository`.

## Логические модули

- `core` - оркестрация, API, lifecycle, журналирование, jobs framework
- `worker-runtime` - отдельные worker-процессы `thelper-worker`, выполняющие queued jobs из persistent storage
- `status-monitor` - логический модуль агрегации job events, workflow statuses, worker health, module health и runtime metrics
- `global-scanner` - обход глобальных `root_path` из `scanning.global_scan`, ignore matcher, обнаружение Terraform-проектов и enqueue фоновых `project_discovery` jobs
- `project-scanner` - project-level Terraform scan по настройкам отдельного проекта; baseline toolchain: `terraform validate`, `TFLint`
- `security-validator` - security/validation scan по настройкам отдельного проекта и доступным модулям из `scanning.security_scan.modules`; MVP requires `Trivy` as the mandatory local scanner; other tools are adapter extensions
- `tool-profile-runtime` - shared execution/compatibility layer for external CLI tools; performs version discovery, profile selection, command execution, output parsing and normalized DTO mapping according to ADR 0018
- `tool-profile-analyzer` - optional maintainer/operator tool for generating candidate profile versions from captured external tool outputs and validation fixtures; generated profiles require explicit validation and activation
- `repository-manager` - `clone`, `pull`, `sync`, защита от конфликтующих операций; webhook/polling operations поставляются как later extensions
- `config-manager` - импорт, валидация и применение конфигурации из БД
- `auth` - authentication providers, `SCIM`, `RBAC`, users, groups, roles, bindings
- `module-runtime` - состояния модулей, `reload`, `restart`
- `web` - поставка `Web UI`
- `gui` - desktop-клиент для локального использования
- `nginx` - reverse proxy, TLS termination, static delivery

## Provider adapters

Provider-specific integrations are pluggable modules or libraries:

- database/storage providers: `sqlite`, `postgresql`, `mysql`, `mssql`;
- auth providers: local auth and future external IAM/SSO providers;
- SCIM providers;
- repository providers: `gitlab`, `github`, `bitbucket`, `azure_devops` and generic Git where explicitly supported;
- policy/tool providers where integration behavior is provider-specific.

Provider-specific code must not live in HTTP handlers, CLI commands or domain logic. Runtime services select providers through configuration and provider registries.

## Сценарии развёртывания

### Монолитный режим

На одном хосте работают:

- `core`
- `worker-runtime` / `thelper-worker`
- `status-monitor`
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

В одной локальной установке активен только один экземпляр `t-helper` runtime:

- если `thelper` уже запущен для `Web UI`, Tauri GUI подключается к существующему local runtime;
- если `thelper` ещё не запущен, Tauri GUI запускает local `thelper`, после чего `Web UI` подключается к этому же runtime;
- повторный запуск `thelper` должен обнаруживать существующий runtime через lock/health mechanism и не создавать второй активный процесс.

### Разнесённый режим

Отдельно могут быть вынесены:

- `global-scanner`
- `project-scanner`
- `security-validator`
- `repository-manager`
- `worker-runtime` / `thelper-worker`
- `status-monitor`
- `auth`
- `web`
- `nginx`
- БД

## Storage strategy

Целевые хранилища по roadmap:

- `SQLite`
- `PostgreSQL`
- `MySQL`
- `MSSQL`

Базовые значения:

- `SQLite` - default internal SQL-like storage for local setup
- `PostgreSQL` - default external storage for server setup
- `MySQL` - optional external storage
- `MSSQL` - optional external storage

Managed engine compatibility:

- Aurora PostgreSQL uses the `PostgreSQL` storage adapter and migration dialect.
- Aurora MySQL uses the `MySQL` storage adapter and migration dialect.
- Babelfish for Aurora PostgreSQL is not treated as the `MSSQL` adapter target unless a separate compatibility decision is accepted.

Требования к слою хранения:

- логическая модель данных едина для всех backends;
- backend использует storage abstraction;
- SQL-хранилища поддерживают dialect-specific миграции с синхронизированными logical migration versions;
- внутренняя БД выбирается через `database.database_type` и `database.database_path`;
- если `external_databases.enabled = true`, runtime использует `external_databases` и не использует внутреннюю БД для рабочих данных;
- `SQLite` и `PostgreSQL` входят в первый обязательный этап реализации;
- `MySQL`, `MSSQL` и другие SQL-compatible adapters поставляются отдельными adapters на Stage 10 Storage Adapter Expansion.

## Jobs и worker execution

Background jobs выполняются отдельными worker-процессами.

Правила:

- `thelper` создаёт jobs, валидирует requests, управляет API/config/module state и не выполняет long-running jobs inline;
- `thelper-worker` атомарно claims eligible queued job через storage-level lease, переводит его в `running`, выполняет job handler, сохраняет `result_payload` и завершает job;
- worker-процессы обновляют `heartbeat_at` и `lease_expires_at` для long-running jobs;
- истёкшие leases восстанавливаются worker-процессами через retry/backoff или перевод job в `failed` после исчерпания попыток;
- `job_locks` используются для сериализации конфликтующих бизнес-операций между несколькими worker-процессами;
- job lease определяет владельца конкретного job, а `job_locks` защищают бизнес-ресурсы;
- `global-scanner`, `project-scanner`, `security-validator`, `repository-manager` и `scim_sync` выполняются через worker execution model;
- worker-процессы могут масштабироваться горизонтально при сохранении единых storage contracts.

## Status monitoring и aggregation

Все jobs должны публиковать status events и runtime metrics в persistent `job_events`.

`status-monitor`:

- агрегирует `job_events`, `jobs`, `module_states` и worker heartbeat data;
- владеет aggregate read models `workflow_statuses`;
- обновляет aggregate `project_scans.status` и `project_scans.result_payload`;
- отдаёт единый источник статусов для UI, backend API и внутренних служб;
- в MVP может быть логическим модулем внутри `thelper`, но должен иметь отдельную границу ответственности;
- в distributed mode может быть вынесен в отдельный процесс или узел.

Правило владения состоянием:

- workers пишут факты выполнения и domain results;
- `status-monitor` пишет aggregate statuses;
- UI и внутренние сервисы не должны самостоятельно агрегировать workflow status из child jobs.

### Repo storage strategy

Для локального размещения клонированных репозиториев используется тот же набор `root_path`, что и для глобального сканирования: `scanning.global_scan`.

Правила:

- при clone пользователь выбирает существующий `root_path` или создаёт новый root path;
- внутри выбранного root path пользователь выбирает существующую target directory или создаёт новую;
- clone workflow использует provider-aware adapters для `gitlab`, `github`, `bitbucket`, `azure_devops`;
- provider integrations use GitKraken-like profiles: one provider can have multiple configured hosts/provider instances, and one host can have multiple credentials with different usages;
- protocol `https|ssh` выбирается на уровне clone request и влияет на итоговый clone URL;
- repository identity строится как `provider + provider_host + full_path`, где `provider_host` различает self-hosted provider instances;
- provider URL parsing follows ADR 0016 and must normalize equivalent HTTPS/SSH/scp-like URLs into the same identity;
- `clone_url` хранится только как nullable non-unique transport metadata без credentials/userinfo и не участвует в deduplication;
- credentials are selected by `credential_id`; workers resolve `secretref://...` values at use time and never receive raw secrets in job payloads;
- recursive GitLab group clone is a later repository operations extension after single-repository clone;
- если clone выполняется в новый path, `repository-manager` добавляет его в список `root_paths`;
- локальный путь репозитория строится внутри выбранного `root_path`;
- `provider_host`, `full_path` и итоговый `local_path` должны быть нормализованы; `local_path` не должен позволять выход за пределы выбранного `root_path`;
- если каталог уже содержит корректный Git-репозиторий с ожидаемым remote, `clone` заменяется на `pull`;
- операции `clone`, `pull` и `sync` по одному `repository.id` сериализуются через `job_locks`.

## Базовый runtime flow

1. `thelper-ctl -reconfigure` импортирует конфигурацию и ignore rules в БД.
2. `thelper` поднимает runtime и читает активную конфигурацию из БД.
3. `global-scanner` запускает глобальное сканирование по `root_path` из `scanning.global_scan`.
4. Обнаруженные Terraform-проекты регистрируются отдельными `projects` rows; для каждого созданного или обновлённого проекта создаётся фоновый `project_discovery` job.
5. `project_discovery` определяет локальные Git-связи проекта, создаёт/обновляет repository card при наличии Git marker и связывает отдельные project records через `project_links`, если они относятся к одному Git repository.
6. `repository-manager` поддерживает `clone`, `pull`, `sync`; при clone в новый path он добавляет этот path в `root_paths`.
7. Stage 06A provides `tool-profile-runtime`, certified profiles, profile validation and optional analyzer for external CLI compatibility.
8. `project-scanner` выполняет project-level Terraform scan по настройкам конкретного проекта с использованием `terraform validate` и `TFLint` через `tool-profile-runtime`.
9. `security-validator` выполняет security/validation scan по настройкам конкретного проекта с использованием `Trivy` через `tool-profile-runtime`, затем сохраняет normalized findings. `Gitleaks`, `Checkov`, `OPA` и `Conftest` подключаются как adapter extensions.
10. `module-runtime` обеспечивает `reload`, `restart` и статусы модулей.

## Ключевые архитектурные риски, устранённые рефакторингом документации

- убрана зависимость от ссылок на номера строк в другом документе;
- продуктовые требования и технические механики разведены по отдельным документам;
- дубли и несовпадающие формулировки сведены к одному источнику истины;
- репозиторий получил устойчивую структуру для последующего code scaffolding.
