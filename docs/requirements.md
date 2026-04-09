# Требования

## Функциональные требования

### Глобальное сканирование

Система должна:

- выполнять scan относительно одного или нескольких `root_path` из `scanning.global_scann`;
- находить Terraform-проекты по наличию `*.tf`;
- находить Git-репозитории по наличию `.git` directory, `.git` file для worktree/submodule или другого явно включённого Git marker;
- не читать содержимое Terraform-файлов, если для обнаружения достаточно имён;
- прекращать углубление после обнаружения Terraform working directory;
- поддерживать ручной запуск, запуск по расписанию и запуск по внутреннему событию;
- выполняться отдельным модулем `global-scanner`.

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
- создавать или обновлять карточку Git-репозитория;
- фиксировать время первого и последнего обнаружения;
- сохранять связи с `repository`, `environment` и `workspace`, если они известны.

### Работа с репозиториями

Система должна поддерживать:

- `clone`
- `pull`
- `sync`
- webhook-based sync
- polling-based sync

Ограничения:

- clone и scan должны использовать один и тот же список `root_path` из `scanning.global_scann`;
- пользователь должен иметь возможность выбрать существующий root path из `scanning.global_scann` как target path для clone;
- пользователь должен иметь возможность создать новый root path при clone;
- новый root path, созданный при clone, должен автоматически добавляться в `scanning.global_scann`;
- внутри выбранного root path пользователь должен иметь возможность выбрать существующую директорию для clone или создать новую;
- локальный путь репозитория должен находиться внутри выбранного root path и выбранной target directory;
- в UI protocol для clone должен выбираться рядом с полем ввода URL и принимать `https` или `ssh`;
- должны поддерживаться разные providers для clone: `gitlab`, `github`, `bitbucket`;
- provider должен влиять на разбор URL, выбор transport protocol и bulk operations;
- должна поддерживаться возможность клонировать один repository и отдельный bulk clone workflow;
- должен поддерживаться bulk clone всех проектов GitLab group с рекурсивным обходом всех вложенных subgroups;
- если локальный каталог уже содержит корректный Git-репозиторий, вместо повторного `clone` выполняется `pull`;
- должны поддерживаться `SSH` и `HTTPS`;
- конфликтующие параллельные операции по одному репозиторию должны блокироваться.

### Базовый стек сканирования

Роли модулей и локально запускаемых инструментов должны быть фиксированы документацией и runtime-конфигурацией:

- `global-scanner` отвечает только за discovery и registry update;
- `project-scanner` использует `terraform validate` и `TFLint` для project-level Terraform checks;
- `security-validator` использует `Trivy`, `Gitleaks`, `Checkov` для security/validation checks;
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

Технологический baseline для `security-validator`:

- `Trivy`
- `Gitleaks`
- `Checkov`

Enterprise-policy checks должны поддерживаться через:

- `OPA`
- `Conftest`

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

### Frontend

Оба интерфейса, `GUI` и `Web UI`, должны использовать единый backend API. Для MVP обязательны основные read/operate сценарии:

- просмотр проектов, environments и workspaces;
- управление scan roots и ignore rules;
- запуск глобального scan и project scan;
- управление repository operations;
- просмотр findings, jobs, module states и audit log;
- просмотр конфигурации.

Расширенные administrative сценарии могут поставляться отдельным hardening-этапом:

- редактирование конфигурации;
- управление users, groups, roles, role bindings и `SCIM`;
- управление security rule sets.

`GUI` работает только локально. Удалённый доступ предоставляется через `Web UI`.

## Нефункциональные требования

### Производительность

- global-scanner минимизирует число операций `stat/open`;
- global-scanner использует worker pool с ограничением параллелизма;
- security-validator использует отдельную очередь задач;
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
