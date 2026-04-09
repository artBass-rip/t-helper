# Roadmap и приёмка

## Этапы реализации

### Этап 1. Foundation

- каркас `thelper`
- каркас `thelper-ctl`
- storage abstraction
- поддержка `PostgreSQL` и `Badger`
- базовые миграции
- базовая конфигурационная модель
- import через `thelper-ctl -reconfigure`
- reload через `thelper-ctl -reload`
- базовый lifecycle модулей
- API skeleton
- базовое логирование и jobs framework

### Этап 2. Scanner MVP

- scan roots
- ignore rules exclude-only с сохранением `!pattern` для будущего full `.gitignore` semantics
- `global-scanner` worker pool
- обнаружение Terraform-проектов по `*.tf`
- определение Git-репозиториев
- прекращение обхода после обнаружения проекта
- регистрация и обновление проектов в БД
- jobs и статусы scan
- API для scan и projects

### Этап 3. Repository Manager MVP

- модель `repositories`
- `clone`
- `pull`
- `sync`
- clone в существующий или новый `root_path`
- выбор target directory внутри `root_path`
- provider-aware clone adapters для `gitlab`, `github`, `bitbucket`
- recursive clone GitLab group/subgroups
- защита от конфликтующих операций
- webhook sync
- polling sync
- API и jobs для repository operations

### Этап 4. Project Scanner и Security Validator MVP

- модуль `project-scanner`
- модуль `security-validator`
- модели `project_scans`, `security_rule_sets`, `security_findings`
- локальный запуск project scans по настройкам отдельного проекта
- локальный запуск security/validation scans по настройкам отдельного проекта
- запуск после `clone` и `pull`, если включён в настройках проекта
- `terraform validate` и `TFLint` как baseline для `project-scanner`
- `Trivy`, `Gitleaks`, `Checkov` как baseline для `security-validator`
- API и jobs для project scans
- локальное хранение findings

### Этап 5. Auth, SCIM, RBAC

- модуль `auth`
- локальная аутентификация
- расширяемые external auth providers
- users, groups, memberships
- roles, permissions, role bindings
- enforcement в API
- `SCIM`
- audit security-событий

### Этап 6. Frontend MVP

- `Web UI`
- локальный `GUI`
- единая frontend codebase
- просмотр проектов, environments и workspaces
- управление scan roots и ignore rules
- запуск global scan и project scan
- управление repository operations
- просмотр findings, jobs, module states, audit log и configuration

### Этап 7. Operational Hardening

- adapters для `MySQL`, `SQLite`, `MongoDB`
- hardening module runtime
- observability
- scheduler hardening
- расширение локальных rule sets
- enterprise-policy packs для `OPA`/`Conftest`
- full `.gitignore` semantics с отрицательными правилами `!pattern`
- UI для auth, RBAC, `SCIM`
- UI для редактирования configuration и security rule sets

### Этап 8. Distributed Deployment

- вынесение `global-scanner`
- вынесение `project-scanner`
- вынесение `repository-manager`
- вынесение `security-validator`
- вынесение `auth`
- согласование межмодульного взаимодействия
- подготовка multi-node и HA deployment

## Критерии приёмки

### MVP acceptance

1. Terraform-проект обнаруживается по наличию `*.tf`.
2. Глобальное сканирование определяет Terraform-проекты и Git-репозитории.
3. После обнаружения проекта вложенные директории не сканируются как отдельные working directories.
4. Ignore rules корректно исключают файлы и директории; `!pattern` сохраняется без потери данных и применяется после реализации full `.gitignore` semantics.
5. Проекты и репозитории сохраняются и обновляются в БД.
6. Project-level scan определяет providers, required auth, syntax issues, deprecations и quality issues через `terraform validate` и `TFLint`, а security/validation scan определяет validate results, secrets и security findings через `Trivy`, `Gitleaks`, `Checkov` и подключённые policy engines.
7. Runtime-конфигурация хранится в БД.
8. `thelper-ctl -reconfigure` импортирует конфигурацию и ignore rules в БД.
9. `thelper-ctl -reload` применяет reloadable-конфигурацию.
10. `thelper-ctl -restart <module>` работает для любого отдельного модуля.
11. `GUI` и `Web UI` используют единый backend API и покрывают MVP read/operate сценарии.
12. `GUI` работает только локально.
13. `PostgreSQL` и `Badger` поддерживаются через storage abstraction.
14. `clone`, `pull`, `sync` не приводят к неконсистентному состоянию и сериализуются через `job_locks`.
15. Поддерживаются `environments` и `workspaces`.
16. `auth` реализован как отдельный модуль.
17. Базовые `SCIM` и `RBAC` контракты реализованы на backend/API уровне.
18. Security stack работает локально и не передаёт код наружу.
19. Security findings и rule sets хранятся внутри системы.
20. Один project scan API создаёт `project_scans`, а security findings читаются через scoped или security endpoints без отдельной сущности `security_scans`.
21. Backend API покрывает scan roots, repositories, jobs, environments, workspaces и module states для Frontend MVP.
22. Clone workflow поддерживает `gitlab`, `github`, `bitbucket`, выбор `https|ssh`, выбор root path и target directory, а также recursive clone GitLab group/subgroups.

### Platform acceptance

Система считается принятой как полная platform release, если дополнительно:

1. Поддерживаются все целевые БД по roadmap: `PostgreSQL`, `Badger`, `MySQL`, `SQLite`, `MongoDB`.
2. Реализован полный administrative UI для auth, RBAC и `SCIM`.
3. Реализован UI для редактирования configuration и security rule sets.
4. Реализована full `.gitignore` semantics с отрицательными правилами `!pattern`.
5. Подготовлен distributed deployment для `global-scanner`, `project-scanner`, `security-validator`, `repository-manager`, `auth`, `web`, `nginx` и БД.

## Открытые вопросы

- включать ли поддержку `terragrunt.hcl` в первую промышленную версию;
- какой стартовый набор локальных security rules и policy packs для `OPA`/`Conftest` должен войти в первую поставку;
- какой минимальный набор external auth providers должен войти в первый релиз;
- нужна ли более строгая модель связей между `project`, `environment` и `workspace`;
- какой технологический стек будет выбран для backend, frontend и desktop GUI;
- требуется ли полноценный full `.gitignore` matcher в первом production release или достаточно exclude-only MVP.
