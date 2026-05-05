# CLI, API и конфигурация

## Исполняемые компоненты

- `thelper` - основной runtime и backend service, реализуемый на `Go`
- `thelper-worker` - отдельный worker process для выполнения queued jobs, реализуемый на `Go`
- `thelper-ctl` - CLI для импорта конфигурации, lifecycle и административных операций, реализуемый на `Go`

Frontend stack для `Web UI` и локального `GUI` зафиксирован в [`technology-stack.md`](technology-stack.md).

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
- `thelper-ctl tool-profiles validate <path>`
- `thelper-ctl tool-profiles import <path>`
- `thelper-ctl tool-profiles activate <tool> <profile_id> <profile_version>`
- `thelper-ctl tool-profiles analyze <samples_path> --baseline <profile_id>`
- `thelper-ctl modules list`

## Минимальный backend API

HTTP conventions, response schemas и endpoint skeleton описаны в [`api.md`](api.md).

- `GET /api/health`
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
- `GET /api/scans/{job_id}` - temporary compatibility endpoint for global scan jobs; canonical status endpoint is `GET /api/jobs/{id}`
- `POST /api/project-scans`
- `GET /api/project-scans/{project_scan_id}`
- `GET /api/project-scans/{project_scan_id}/findings`
- `GET /api/jobs`
- `GET /api/jobs/{id}`
- `GET /api/status`
- `GET /api/status/workflows`
- `GET /api/status/workflows/{job_group_id}`
- `GET /api/status/jobs/{job_id}`
- `GET /api/status/workers`
- `GET /api/config`
- `PUT /api/config`
- `GET /api/ignore-rules`
- `PUT /api/ignore-rules`
- `GET /api/repos`
- `GET /api/repos/{id}`
- `GET /api/repo-provider-instances`
- `PUT /api/repo-provider-instances`
- `GET /api/repo-credentials`
- `PUT /api/repo-credentials`
- `POST /api/repos/clone`
- `POST /api/repos/pull`
- `POST /api/repos/sync`
- `GET /api/security/findings`
- `GET /api/security/findings/{id}`
- `GET /api/security/rule-sets`
- `PUT /api/security/rule-sets`
- `GET /api/tool-profiles`
- `POST /api/tool-profiles/validate`
- `POST /api/tool-profiles/import`
- `POST /api/tool-profiles/activate`
- `POST /api/tool-profiles/analyze`
- `GET /api/modules`
- `POST /api/modules/reload`
- `POST /api/modules/restart`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/session`
- `POST /api/auth/password-reset/request`
- `POST /api/auth/password-reset/confirm`
- `POST /api/auth/password/change`
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
- Project scan запускается как parent-child workflow: parent `project_scan` job создаёт child `security_validation_scan` jobs при включённых security modules; все jobs связываются через `job_group_id`.
- `status-monitor` агрегирует статусы workflows/jobs, а UI и внутренние сервисы читают aggregate status через documented status/project scan endpoints.
- Security findings читаются через `/api/security/findings` или через scoped endpoint `/api/project-scans/{project_scan_id}/findings`.
- Отдельный `/api/security/scans` не вводится, чтобы не дублировать `project_scans` в модели данных.
- `GET /api/health` является confirmed safe unauthenticated endpoint для локального discovery singleton runtime и не раскрывает config, paths, DSN, secrets, users или object-scoped details.
- Runtime auth endpoints (`login`, `logout`, `session`, password reset/change) отделены от administrative auth endpoints (`users`, `groups`, `roles`, `role-bindings`, `SCIM`).
- Write endpoints, которые запускают фоновые операции, создают `jobs` и возвращают `job_id`.
- Write endpoints, которые меняют справочники или конфигурацию, обновляют сущности идемпотентно там, где это применимо.
- Confirmed MVP behavior: bulk `PUT` endpoints are non-destructive idempotent upserts by stable identity or `id`; omitted records are not deleted.
- Confirmed MVP behavior: public `DELETE` endpoints are out of scope for MVP even though delete permissions are seeded for future lifecycle expansion.
- Confirmed MVP lifecycle behavior: administrative removal is represented by explicit non-destructive state transitions such as disabling/deactivating records, marking projects missing or superseding repositories. Hard delete requires a future explicit API contract.
- Repository operations должны использовать `job_locks` для сериализации `clone`, `pull` и `sync` по одному `repository.id`.
- Stage 05 MVP conflict policy: если для `lock_key = repository:<id>` уже существует `queued` или `running` `clone`, `pull` или `sync` job, новый repository operation request возвращает `409 conflict` с кодом `repository_operation_already_running`, кроме exact replay с тем же `Idempotency-Key`, который возвращает существующий `job_ref`. `clone` additionally checks normalized repository identity and normalized target path conflicts before a stable `repository_id` exists.
- Long-running operations выполняются отдельными `thelper-worker` процессами; backend API создаёт jobs и не выполняет их inline.
- `GET/PUT /api/repo-provider-instances` управляет GitKraken-like integration profiles для cloud/on-premise/multi-domain Git hosts.
- `GET/PUT /api/repo-credentials` управляет multi-credential records per provider host; API принимает только `secretref://...`, raw secrets отклоняются.
- `POST /api/repos/clone` должен принимать provider-aware request: `provider_instance_id` или `provider` + optional/derived `provider_host`, optional `credential_id`, выбор `root_path_id` или `new_root_path`, выбор существующей `target_directory` или `new_target_directory`, а также явный `protocol = https|ssh`.
- Repository identity нормализуется как `provider + provider_host + full_path`; `clone_url` nullable, non-unique и не используется для lookup/upsert/deduplication.
- Persisted `clone_url` должен быть safe-normalized и не должен содержать credentials, tokens, passwords или URL userinfo.
- Provider URL parsing для `gitlab`, `github`, `bitbucket`, `azure_devops` следует ADR 0016; equivalent HTTPS/SSH/scp-like URLs должны давать одинаковую identity.
- Для MVP clone/pull credential должен поддерживать `git_transport`. Для GitLab recursive group clone credential должен поддерживать usage `provider_api`, а для webhook verification - `webhook`; эти workflows поставляются после Stage 05 MVP.
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
- `workers`
- `modules`
- `logging`

Подробная структура и правила валидации описаны в [`configuration.md`](configuration.md).

Project-level scan и security/validation scan настраиваются относительно отдельного проекта. В глобальной конфигурации `scanning.security_scan.modules` задаёт только список security/validation модулей и policy engines, доступных для подключения к проекту. Базовый стек: Stage 06A поставляет tool profile runtime; Stage 06B `project-scanner` использует `terraform validate` и `TFLint`, а `security-validator` использует `Trivy` как обязательный scanner; `Gitleaks`, `Checkov`, `OPA`/`Conftest` подключаются как adapter extensions.

### Базовый стек сканирования

- `global-scanner` - discovery-only модуль без чтения raw Terraform source сверх необходимого для обнаружения.
- `project-scanner` - orchestration layer для `terraform validate` и `TFLint`.
- `security-validator` - orchestration layer для mandatory MVP `Trivy` scanner and future scanner adapters.
- `OPA`/`Conftest` - optional policy engines для enterprise-rule sets after MVP scanner contract.

Минимальные ключи для `repositories`:

- `default_auth_type`
- `poll_interval_default`
- `auto_sync_default`

## Поведение конфигурации

### `thelper-ctl -reconfigure`

Команда должна:

- считать `config.json`;
- считать `.t-helper.ignore`;
- валидировать входные данные через strict schema contract;
- отклонять unknown keys, deprecated aliases и sensitive literal values;
- принимать только `scanning.global_scan` для global scan roots;
- атомарно записать конфигурацию и ignore rules в БД;
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

1. Получить активные `root_path`, импортированные из `scanning.global_scan`, и ignore rules из БД.
2. Построить matcher исключений.
3. Начать обход директорий.
4. Для каждой директории проверить ignore rules.
5. Прочитать только список имён текущего уровня.
6. Если найден `*.tf`, создать или обновить отдельную запись Terraform-проекта, поставить `project_discovery` job и не углубляться ниже.
7. Продолжить обход только разрешённых поддиректорий, не ожидая завершения `project_discovery`.

Global scan не определяет Git repository identity и не регистрирует Git repository напрямую. Git marker allowlist (`.git/` directory или `.git` file с первой непустой строкой `gitdir:`) применяется фоновым `project_discovery` job. Не считать Git markers: `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` и другие похожие convention/config files. Global scan и `project_discovery` не должны выполнять shell-out в `git`.

## Оптимизации сканера

- использовать directory entry API;
- не открывать `*.tf`, если достаточно имени файла;
- кэшировать ignore matcher на время job;
- симлинки обходить только в явно включённом режиме.
