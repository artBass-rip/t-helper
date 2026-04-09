# CLI, API и конфигурация

## Исполняемые компоненты

- `thelper` - основной runtime и backend service
- `thelper-ctl` - CLI для импорта конфигурации, lifecycle и административных операций

## Обязательные команды `thelper-ctl`

- `thelper-ctl -reconfigure`
- `thelper-ctl -reload`
- `thelper-ctl -restart <module>`

## Рекомендуемые команды

- `thelper-ctl scan start`
- `thelper-ctl scan status`
- `thelper-ctl project-scan start <project>`
- `thelper-ctl project-scan status <project|job>`
- `thelper-ctl repos clone <url>`
- `thelper-ctl repos pull <project|repo>`
- `thelper-ctl repos sync <project|repo>`
- `thelper-ctl modules list`

## Минимальный backend API

HTTP conventions, response schemas и endpoint skeleton описаны в [`api.md`](api.md).

- `GET /api/root-paths`
- `PUT /api/root-paths`
- `GET /api/projects`
- `GET /api/projects/{id}`
- `GET /api/projects/{id}/scan-settings`
- `PUT /api/projects/{id}/scan-settings`
- `GET /api/environments`
- `GET /api/environments/{id}`
- `GET /api/workspaces`
- `GET /api/workspaces/{id}`
- `POST /api/scans`
- `GET /api/scans/{job_id}`
- `POST /api/project-scans`
- `GET /api/project-scans/{project_scan_id}`
- `GET /api/project-scans/{project_scan_id}/findings`
- `GET /api/jobs`
- `GET /api/jobs/{id}`
- `GET /api/config`
- `PUT /api/config`
- `GET /api/ignore-rules`
- `PUT /api/ignore-rules`
- `GET /api/repos`
- `GET /api/repos/{id}`
- `POST /api/repos/clone`
- `POST /api/repos/pull`
- `POST /api/repos/sync`
- `GET /api/security/findings`
- `GET /api/security/findings/{id}`
- `GET /api/security/rule-sets`
- `PUT /api/security/rule-sets`
- `GET /api/modules`
- `POST /api/modules/reload`
- `POST /api/modules/restart`
- `GET /api/auth/users`
- `PUT /api/auth/users`
- `GET /api/auth/groups`
- `PUT /api/auth/groups`
- `GET /api/auth/roles`
- `PUT /api/auth/roles`
- `GET /api/auth/role-bindings`
- `PUT /api/auth/role-bindings`
- `GET /api/auth/scim/identities`
- `POST /api/auth/scim/sync`
- `GET /api/audit`

### API decisions

- `POST /api/project-scans` является единым endpoint для запуска project-level Terraform scan и подключённых к проекту security/validation checks.
- Security findings читаются через `/api/security/findings` или через scoped endpoint `/api/project-scans/{project_scan_id}/findings`.
- Отдельный `/api/security/scans` не вводится, чтобы не дублировать `project_scans` в модели данных.
- Write endpoints, которые запускают фоновые операции, создают `jobs` и возвращают `job_id`.
- Write endpoints, которые меняют справочники или конфигурацию, обновляют сущности идемпотентно там, где это применимо.
- Repository operations должны использовать `job_locks` для сериализации `clone`, `pull` и `sync` по одному `repository.id`.
- `POST /api/repos/clone` должен принимать provider-aware request: выбор `root_path_id` или `new_root_path`, выбор существующей `target_directory` или `new_target_directory`, а также явный `protocol = https|ssh`.
- UI clone workflow должен показывать protocol selector рядом с URL field.
- Для `provider = gitlab` должен поддерживаться отдельный режим `clone_scope = gitlab_group_recursive`.

## Конфигурационная модель

После первичного импорта source of truth:

- БД

Файлы первичного импорта:

- `config.json`
- `.t-helper.ignore`

`config.example.json` поставляется как валидный референс для структуры `config.json`.

Логические секции конфигурации:

- `system_settings`
- `database`
- `external_databases`
- `scanning`
- `repositories`
- `security`
- `api`
- `auth`
- `modules`
- `logging`

Подробная структура и правила валидации описаны в [`configuration.md`](configuration.md).

Project-level scan и security/validation scan настраиваются относительно отдельного проекта. В глобальной конфигурации `scanning.security_scan.modules` задаёт только список security/validation модулей и policy engines, доступных для подключения к проекту. Базовый стек: `project-scanner` использует `terraform validate` и `TFLint`, `security-validator` использует `Trivy`, `Gitleaks`, `Checkov`, а enterprise-policy checks подключаются через `OPA`/`Conftest`.

### Базовый стек сканирования

- `global-scanner` - discovery-only модуль без чтения raw Terraform source сверх необходимого для обнаружения.
- `project-scanner` - orchestration layer для `terraform validate` и `TFLint`.
- `security-validator` - orchestration layer для `Trivy`, `Gitleaks`, `Checkov`.
- `OPA`/`Conftest` - optional policy engines для enterprise-rule sets.

Минимальные ключи для `repositories`:

- `default_auth_type`
- `poll_interval_default`
- `auto_sync_default`

## Поведение конфигурации

### `thelper-ctl -reconfigure`

Команда должна:

- считать `config.json`;
- считать `.t-helper.ignore`;
- валидировать входные данные;
- записать конфигурацию и ignore rules в БД;
- не запускать сервис;
- не перезапускать сервис напрямую.

### `thelper-ctl -reload`

Команда должна:

- инициировать перечитывание конфигурации из БД;
- применить reloadable-параметры без полного рестарта;
- явно сообщать о параметрах, требующих рестарта.

### `thelper-ctl -restart <module>`

Команда должна:

- перезапускать любой отдельный модуль;
- работать с расширяемым списком модулей;
- обновлять состояние модуля в `module_states`.

## Алгоритм глобального сканирования

1. Получить активные `root_path`, импортированные из `scanning.global_scann`, и ignore rules из БД.
2. Построить matcher исключений.
3. Начать обход директорий.
4. Для каждой директории проверить ignore rules.
5. Прочитать только список имён текущего уровня.
6. Если найден `*.tf`, зарегистрировать Terraform-проект и не углубляться ниже.
7. Если найден `.git`, зарегистрировать Git-репозиторий.
8. Продолжить обход только разрешённых поддиректорий.

## Оптимизации сканера

- использовать directory entry API;
- не открывать `*.tf`, если достаточно имени файла;
- кэшировать ignore matcher на время job;
- симлинки обходить только в явно включённом режиме.
