# Требования

## Функциональные требования

### Глобальное сканирование

Система должна:

- выполнять scan относительно одного или нескольких `root_path` из `scanning.global_scan`;
- находить Terraform-проекты по наличию `*.tf`;
- не читать содержимое Terraform-файлов, если для обнаружения достаточно имён;
- прекращать углубление после обнаружения Terraform working directory;
- создавать или обновлять отдельную запись `projects` для каждого локально обнаруженного Terraform working directory;
- ставить фоновый `project_discovery` job для каждого созданного или обновлённого проекта и продолжать filesystem scan без ожидания этого job;
- использовать `follow_symlinks = false` как единственный поддерживаемый MVP-режим обхода; расширенный symlink traversal переносится на Stage 09 runtime hardening;
- поддерживать ручной запуск, запуск по расписанию и запуск по внутреннему событию;
- выполняться отдельным модулем `global-scanner`.

### Project discovery и Git-связи

Система должна:

- выполнять фоновый `project_discovery` job для отдельного проекта;
- определять, является ли проект частью Git working tree, только в `project_discovery`, а не в blocking path глобального scan;
- находить Git-репозитории только по MVP Git marker allowlist: `.git/` directory или `.git` regular file для worktree/submodule;
- для `.git` file читать только Git metadata file и считать marker валидным, если первая непустая строка начинается с `gitdir:`;
- не считать Git marker'ами `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` и другие похожие файлы/директории;
- не merge'ить разные project records, даже если они относятся к одному Git repository;
- помечать проекты из одного Git repository как связанные через `project_links.link_type = same_repository` и общий `repository_id`, если repository уже известен.

### Исключения при сканировании

Система должна:

- поддерживать `.gitignore`-подобный формат правил;
- применять правила относительно `root_path`;
- поддерживать glob-маски для файлов и директорий;
- импортировать правила из `.t-helper.ignore`;
- хранить и редактировать правила через БД и frontend;
- сохранять отрицательные правила `!pattern` без потери данных и применять их после реализации full `.gitignore` semantics.

### Регистрация объектов

После обнаружения система должна:

- создавать или обновлять карточку Terraform-проекта;
- заполнять карточку проекта постепенно: неизвестные на момент global scan поля остаются nullable/default и обновляются последующими discovery/scanning/management stages;
- создавать или обновлять карточку Git-репозитория только в рамках фонового project discovery или repository-manager workflows;
- фиксировать время первого и последнего обнаружения;
- не удалять project records при исчезновении пути; если проект не найден повторным completed scan, переводить его в `status = missing`;
- автоматически возвращать project record в `status = active`, если тот же `root_path_id + relative_path` найден снова;
- сохранять связи с `repository`, `environment` и `workspace`, если они известны.

### Lifecycle без hard delete в MVP

MVP не предоставляет public hard delete endpoints. Удаление из пользовательских
сценариев выражается через явные non-destructive состояния:

- root paths and provider/configuration records disable/deactivate through
  `enabled = false` или эквивалентное active-state field;
- projects use `status = missing` для отсутствующих путей и `status = disabled`
  для административного отключения;
- repositories use `status = disabled`, `missing` или `superseded`;
- users use `active = false`;
- omitted records in bulk `PUT` requests are never deleted.

Будущие hard delete/archive endpoints требуют отдельного API contract,
permissions, migration behavior and tests.

### Работа с репозиториями

Система должна поддерживать в roadmap:

- `clone`
- `pull`
- `sync`
- webhook-based sync
- polling-based sync

MVP repository-manager поддерживает `clone`, `pull`, `sync` для `generic` Git и одного managed provider из `gitlab` или `github`. Polling sync, recursive GitLab group clone, `bitbucket` и `azure_devops` adapters переносятся в repository extension stages, а webhook sync - в Stage 14.

Ограничения:

- clone и scan должны использовать один и тот же список `root_path` из `scanning.global_scan`;
- пользователь должен иметь возможность выбрать существующий root path из `scanning.global_scan` как target path для clone;
- пользователь должен иметь возможность создать новый root path при clone;
- новый root path, созданный при clone, должен автоматически добавляться в `scanning.global_scan`;
- внутри выбранного root path пользователь должен иметь возможность выбрать существующую директорию для clone или создать новую;
- локальный путь репозитория должен находиться внутри выбранного root path и выбранной target directory;
- в UI protocol для clone должен выбираться рядом с полем ввода URL и принимать `https` или `ssh`;
- должны поддерживаться разные providers для clone по roadmap: `gitlab`, `github`, `bitbucket`, `azure_devops`; MVP включает `generic` Git и один managed provider из `gitlab` или `github`;
- integration UX должен поддерживать cloud и on-premise/multi-domain provider hosts: GitHub, GitHub Enterprise Server, GitLab, GitLab Self-Managed, Bitbucket, Bitbucket Data Center и Azure DevOps;
- для одного provider должно поддерживаться несколько hosts/provider instances;
- для одного provider host должно поддерживаться несколько credentials с разными usages/permissions;
- provider должен влиять на разбор URL, выбор transport protocol и bulk operations;
- identity репозитория должен определяться как `provider + provider_host + full_path`, чтобы поддерживать несколько providers и self-hosted instances;
- `clone_url` должен рассматриваться как nullable non-unique transport endpoint, а не как ключ идентичности или deduplication;
- persisted `clone_url` не должен содержать credentials, tokens, passwords или URL userinfo;
- repository operations должны принимать explicit `credential_id` или использовать repository default credential;
- credentials должны хранить только `secretref://...` references, а не raw secret values;
- должна поддерживаться возможность клонировать один repository и отдельный bulk clone workflow;
- bulk clone всех проектов GitLab group с рекурсивным обходом всех вложенных subgroups переносится после single-repository clone MVP;
- если локальный каталог уже содержит корректный Git-репозиторий, вместо повторного `clone` выполняется `pull`;
- должны поддерживаться `SSH` и `HTTPS`;
- конфликтующие параллельные операции по одному репозиторию должны блокироваться.

### Базовый стек сканирования

Роли модулей и локально запускаемых инструментов должны быть фиксированы документацией и runtime-конфигурацией:

- `global-scanner` отвечает за Terraform project discovery, project registry update и enqueue фоновых `project_discovery` jobs;
- `project-scanner` использует `terraform validate` и `TFLint` для project-level Terraform checks;
- `security-validator` использует `Trivy` как обязательный MVP scanner; `Gitleaks`, `Checkov`, `OPA` и `Conftest` подключаются как extensions;
- enterprise-policy checks подключаются через `OPA` и `Conftest`;
- findings, rules и policies остаются локальными и не отправляются во внешние сервисы.

### Проектное Terraform-сканирование

Project-level scan запускается вручную, по расписанию, после `clone`, после `pull` или по внутреннему событию.

Настройки project-level scan задаются относительно отдельного проекта. Глобальная конфигурация не должна содержать default-настройки project scan.

Project-level scan выполняется отдельным модулем `project-scanner`.

Технологический baseline для `project-scanner`:

- `terraform validate`
- `TFLint`

Минимальный набор проверок:

- `terraform.providers`
- `terraform.required_auth`
- `terraform.syntax`
- `terraform.deprecations`
- `terraform.quality`
- `terraform.module_source`
- `terraform.provider_usage`
- `terraform.policy`

### Сканирование безопасности и валидация кода

Security/validation scan запускается относительно отдельного проекта и выполняется отдельным модулем `security-validator`.

Технологический roadmap baseline для `security-validator`:

- `Trivy`
- `Gitleaks`
- `Checkov`

Enterprise-policy checks должны поддерживаться через:

- `OPA`
- `Conftest`

MVP requires `Trivy` as the mandatory local security scanner. `Gitleaks`, `Checkov`, `OPA` and `Conftest` are adapter extension points and are not mandatory MVP acceptance.

Система должна:

- хранить в глобальной конфигурации `scanning.security_scan.modules` только список доступных модулей проверок и policy engines;
- разрешать подключение доступных security/validation модулей в настройках конкретного проекта;
- не запускать модуль проверки для проекта, если он не включён в проектной настройке;
- выполнять проверки `terraform.validate`, `terraform.secrets`, `terraform.security.misconfig`, `terraform.policy` и расширяемые security/validation modules;
- сохранять findings с привязкой к проекту, rule set, job и времени обнаружения.

Ожидаемый результат:

- список providers, их версий и aliases;
- metadata о требуемой авторизации;
- findings по syntax, deprecations, quality и security;
- fingerprinted результаты с привязкой к rule set и времени обнаружения.

### Конфигурация и lifecycle

Система должна:

- хранить runtime-конфигурацию в БД;
- валидировать конфигурацию перед записью;
- применять reloadable-настройки через `thelper-ctl -reload`;
- поддерживать `thelper-ctl -restart <module>` для любого отдельного модуля;
- показывать состояния модулей через UI и CLI.
- выполнять background jobs в отдельных `thelper-worker` процессах, а не inline внутри API runtime.

### Frontend

Оба интерфейса, `GUI` и `Web UI`, должны использовать единый backend API. Для MVP обязательны основные read/operate сценарии:

- просмотр проектов, environments и workspaces;
- управление scan roots и ignore rules;
- запуск глобального scan и project scan;
- управление repository operations;
- просмотр findings, jobs, module states и audit log;
- просмотр конфигурации.

Технологический стек frontend:

- `Web UI`: `React`, `TypeScript`, `Vite`, `TanStack Router`, `TanStack Query`, `Zod`, `React Hook Form`, `Ant Design`;
- локальный `GUI`: `Tauri` поверх той же React/TypeScript codebase.

Stage 08 includes full MVP administrative screens for backend APIs that are in
the target MVP release scope. Platform-only administrative hardening and
capabilities outside the MVP backend scope are delivered separately:

- full SCIM sync management переносится в Stage 13;
- advanced tool profile administration and platform-only controls can be
  hardened in Stage 12;
- bundled policy pack management depends on Stage 11 scope.

`GUI` работает только локально. Удалённый доступ предоставляется через `Web UI`.

В одной локальной установке активен только один `t-helper` runtime:

- если `thelper` уже запущен для `Web UI`, Tauri GUI подключается к существующему runtime;
- если `thelper` ещё не запущен, Tauri GUI запускает local `thelper`, а `Web UI` затем подключается к тому же runtime;
- повторный запуск `thelper` не должен создавать второй активный runtime.

## Нефункциональные требования

### Производительность

- global-scanner минимизирует число операций `stat/open`;
- global-scanner использует worker pool с ограничением параллелизма;
- security-validator использует отдельную очередь задач;
- background jobs выполняются отдельными worker-процессами;
- глобальное и проектное сканирование выполняются независимо.

### Масштабируемость

- поддерживаются несколько путей глобального сканирования;
- модули логически изолированы;
- `global-scanner`, `project-scanner`, `security-validator`, `repository-manager`, `auth` допускают вынос в отдельные процессы или узлы.

### Надёжность

- ошибки отдельных директорий не прерывают весь scan job;
- ошибки отдельных project checks не прерывают глобальный scan и repository operations;
- повторные scan, clone, pull, sync должны приводить систему к тому же целевому состоянию при неизменных входных данных;
- повторные operations не должны создавать дубли `projects`, `repositories`, `jobs` и активных `job_locks`;
- административные и фоновые операции должны журналироваться.

### Безопасность

- frontend требует аутентификацию;
- local auth должен поддерживать login, logout, current session, password reset и password change через documented backend API;
- доступ к операциям контролируется через `RBAC`;
- секреты БД и Git не хранятся в открытом виде;
- код и findings не отправляются наружу;
- security rules и policies используются только локально;
- должен вестись audit log.

## Ограничения MVP

- Terraform-проект определяется только по наличию `*.tf`;
- базовый ignore matcher в MVP может быть exclude-only, поддержка `!pattern` допускается отдельным расширением;
- поддержка `terragrunt.hcl` не обязательна для первой версии;
- runtime verification через `terraform init` и `terraform validate` может быть вынесена в отдельный worker;
- `GUI` не является удалённым клиентом.
