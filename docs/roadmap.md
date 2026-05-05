# Roadmap и приёмка

## Оптимизированные stage реализации

Декомпозиция уменьшает размер foundation-части и делает каждый stage проверяемым отдельно. Детальные implementation specs находятся в `docs/implementation-specs/`.

### Stage 00. Delivery contract

- зафиксировать Definition of Done для MVP и platform release;
- присвоить открытым продуктовым решениям статус `accepted`, `deferred` или `out-of-scope` в Stage 00 decision register;
- зафиксировать структуру будущего code repository, style guides, CI/scaffolding checklist и правила обновления трассировки как документационный contract;
- нормализовать референсные примеры конфигурации и секретов.

Executable scaffolding, actual CI files, Go modules and storage test harnesses
are Stage 01 implementation deliverables. Stage 00 is considered complete when
the delivery decisions and documentation contracts are accepted.

### Stage 01. Backend storage foundation

- создать Go-модули `thelper`, `thelper-worker`, `thelper-ctl`;
- реализовать HTTP adapter skeleton на `net/http`/`chi`;
- реализовать storage abstraction и миграционный каркас;
- поддержать `SQLite` и `PostgreSQL` для stage-owned системных таблиц Stage 01;
- добавить базовое логирование, correlation IDs и health endpoint.

### Stage 02. Config, modules and runtime lifecycle

- реализовать `config_entries`, `module_states`, import через `thelper-ctl -reconfigure`;
- реализовать reloadability matrix и `thelper-ctl -reload`;
- реализовать module registry с lifecycle `start`, `stop`, `reload`, `health`;
- реализовать `thelper-ctl -restart <module>`;
- зафиксировать singleton runtime lock/health mechanism для локального режима.

### Stage 03. Jobs, workers and status foundation

- реализовать `jobs`, `job_locks`, `job_events`, `workflow_statuses`;
- реализовать atomic job claim, leases, heartbeat, expired lease recovery и retry/backoff;
- вынести выполнение long-running operations в `thelper-worker`;
- реализовать worker handlers минимум для `config_reload` и `module_restart`;
- поднять базовый `status-monitor` для jobs/workers/modules.

### Stage 04. Scanner and registry MVP

- реализовать `root_paths`, `ignore_rules`, `projects`, `environments`, `workspaces` и минимальный `repositories` registry;
- реализовать exclude-only ignore matcher с сохранением `!pattern`;
- реализовать discovery Terraform-проектов по `*.tf` без чтения исходников;
- реализовать enqueue фоновых `project_discovery` jobs для определения Git-связей проекта без блокировки глобального scan;
- реализовать обнаружение Git markers внутри `project_discovery` и прекращение обхода ниже найденного Terraform-проекта в global scan;
- реализовать `project_links` для связи отдельных project records, относящихся к одному Git repository, без merge project rows;
- закрыть API для scan roots, ignore rules, scans, projects, environments и workspaces.

### Stage 05. Repository manager MVP

- полноценно реализовать модель `repositories`;
- реализовать `clone`, `pull`, `sync` через jobs и `job_locks`;
- обеспечить path safety: нормализация, запрет path traversal, `local_path` только внутри выбранного `root_path`;
- зафиксировать repository identity как `provider + provider_host + full_path`;
- реализовать GitKraken-like provider integration profiles: multi-host и multi-credential per host;
- реализовать MVP adapters для `generic` Git и одного из managed providers: `gitlab` или `github`;
- оставить `bitbucket`, `azure_devops`, recursive GitLab group clone, webhook sync и polling sync вне Stage 05 MVP.

### Stage 05A. Repository operations extensions

- добавить второй managed provider из пары `gitlab`/`github`, если он не был включён в Stage 05;
- реализовать recursive clone GitLab group/subgroups после стабилизации single-repository clone;
- добавить provider adapters для `bitbucket` и `azure_devops`;
- сохранить тот же repository identity, credential и path safety contract, что и Stage 05.

### Stage 05B. Repository polling sync

- реализовать polling-based sync как отдельный repository workflow;
- добавить scheduler integration для polling;
- проверить, что polling не обходит `job_locks`, credential usage validation и status-monitor aggregation.

### Stage 06. Project scanner and security validator MVP

- реализовать `project_scan_settings`, `project_security_scan_settings`, `project_scans`;
- реализовать parent-child workflow `project_scan` -> `security_validation_scan` через `job_group_id`;
- подключить `terraform validate` и `TFLint` для `project-scanner`;
- подключить `Trivy` как обязательный security scanner для MVP;
- оставить `Gitleaks`, `Checkov`, `OPA` и `Conftest` как adapter extension points без обязательной MVP-приёмки;
- сохранять `security_rule_sets`, `security_findings` и aggregate status через `status-monitor`.

Stage 06 intentionally split into two delivery packages:

- Stage 06A: external toolchain profile runtime, registry, validation and certified bundled profiles for `terraform validate`, `TFLint` and `Trivy`;
- Stage 06B: project scanner and security validator orchestration using only the Stage 06A profile runtime.

### Stage 07. Auth, RBAC, SCIM and audit

- реализовать локальную аутентификацию и каркас external auth providers;
- реализовать local credentials storage с Argon2id PHC password hashes по ADR 0014;
- реализовать first-run bootstrap admin user flow;
- реализовать users, groups, memberships, roles, permissions и role bindings;
- включить API authorization enforcement по matrix из `access-control.md`;
- реализовать SCIM contract/stub без полноценного sync workflow в MVP;
- писать audit security-события.

### Stage 08. Frontend MVP and local GUI

- реализовать единый React/TypeScript frontend для `Web UI` и `GUI`;
- использовать Vite, TanStack Router, TanStack Query, Zod, React Hook Form и Ant Design;
- покрыть основные read/operate сценарии: projects, roots, ignore rules, scans, repos, findings, jobs, modules, audit, config;
- реализовать полноценные administrative screens для auth/RBAC/configuration/security rule sets, так как соответствующие backend APIs входят в целевой MVP release scope;
- реализовать Tauri GUI только для локального режима;
- проверить, что `GUI` и `Web UI` используют только documented backend API;
- обеспечить feature parity для MVP read/operate и MVP administrative scenarios;

Stage 08 entry contract for route map, navigation, operational density and
Tauri local runtime discovery/start policy is accepted in
[`frontend-ui-contract.md`](frontend-ui-contract.md). Packaging/signing and
update/distribution policy remain release artifact exit decisions.

### Stage 09. Runtime and observability hardening

- расширить observability и `status-monitor`;
- усилить scheduler, worker shutdown/recovery и module runtime;
- реализовать full `.gitignore` semantics с `!pattern`;
- реализовать optional `follow_symlinks = true` hardening with cycle detection, root containment checks and traversal guards, если этот режим утверждён;
- формализовать degraded states, retention cleanup и operator diagnostics для jobs, locks, modules, workers and scans.

### Stage 10. Storage adapter expansion

- добавить `MySQL`, `MSSQL` и другие утверждённые SQL-compatible adapters;
- поставить dialect-specific migrations with synchronized logical migration versions;
- расширить shared storage contract suite на все target adapters;
- задокументировать dialect-specific behavior differences и application-level validation fallback.

### Stage 11. Security tooling and policy packs

- определить и поставить baseline локальных rule sets и enterprise-policy packs;
- подключить дополнительные security adapters: `Gitleaks`, `Checkov`, `OPA` и `Conftest`;
- использовать ADR 0018 tool profiles для дополнительных tools;
- проверить локальность security stack и стабильность ADR 0017 findings fingerprints.

### Stage 12. Admin UI hardening

- hardened extensions for administrative UI beyond the Stage 08 full MVP admin screens;
- добавить UI для advanced tool profile administration, SCIM sync management and platform-only administrative workflows where in release scope;
- добавить SCIM visibility and sync operation surfaces where backend APIs are available;
- сохранить `Web UI`/`GUI` parity по `docs/frontend-ui-contract.md`.

### Stage 13. SCIM full sync

- реализовать полноценный SCIM sync workflow поверх Stage 07 contract/stub;
- зафиксировать conflict policy, mapping rules, audit events and partial failure behavior;
- подключить scheduled/manual sync через jobs, worker leases and status-monitor.

### Stage 14. Repository webhook sync

- реализовать webhook-based repository sync;
- добавить provider-specific webhook payload validation and secret verification;
- enqueue repository sync jobs without bypassing `job_locks`;
- обеспечить audit/status integration for webhook events.

### Stage 15. Distributed deployment

- вынести `global-scanner`, `project-scanner`, `repository-manager`, `security-validator`, `auth` и `status-monitor` в совместимые runtime modes;
- формализовать межмодульные retry, timeout, idempotency и auth contracts;
- подготовить service discovery, health model, worker groups и HA topology;
- проверить, что distributed mode не вводит второй source of truth и не ломает API/storage contracts.

## Критерии приёмки

### MVP acceptance

| ID | Criterion |
| --- | --- |
| `ACC-MVP-001` | Terraform-проект обнаруживается по наличию `*.tf`. |
| `ACC-MVP-002` | Глобальное сканирование определяет Terraform-проекты, а фоновый `project_discovery` определяет Git-связи проектов. |
| `ACC-MVP-003` | После обнаружения проекта вложенные директории не сканируются как отдельные working directories. |
| `ACC-MVP-004` | Ignore rules корректно исключают файлы и директории; `!pattern` сохраняется без потери данных и применяется после реализации full `.gitignore` semantics. |
| `ACC-MVP-005` | Проекты сохраняются отдельными записями, а Git-связи проектов сохраняются без merge project records. |
| `ACC-MVP-006` | Project-level scan определяет providers, required auth, syntax issues, deprecations и quality issues через `terraform validate` и `TFLint`, а security/validation scan сохраняет findings через `Trivy` как обязательный локальный scanner. |
| `ACC-MVP-007` | Runtime-конфигурация хранится в БД. |
| `ACC-MVP-008` | `thelper-ctl -reconfigure` импортирует конфигурацию и ignore rules в БД. |
| `ACC-MVP-009` | `thelper-ctl -reload` применяет reloadable-конфигурацию. |
| `ACC-MVP-010` | `thelper-ctl -restart <module>` работает для любого отдельного модуля. |
| `ACC-MVP-011` | `GUI` и `Web UI` используют единый backend API и покрывают MVP read/operate сценарии. |
| `ACC-MVP-012` | `GUI` работает только локально. |
| `ACC-MVP-013` | `PostgreSQL` и `SQLite` поддерживаются через storage abstraction. |
| `ACC-MVP-014` | `clone`, `pull`, `sync` не приводят к неконсистентному состоянию и сериализуются через `job_locks`. |
| `ACC-MVP-015` | Поддерживаются `environments` и `workspaces`. |
| `ACC-MVP-016` | `auth` реализован как отдельный модуль. |
| `ACC-MVP-017` | Local auth и `RBAC` реализованы на backend/API уровне; SCIM endpoints могут быть contract/stub без полноценного sync workflow. |
| `ACC-MVP-018` | Security stack работает локально и не передаёт код наружу. |
| `ACC-MVP-019` | Security findings и rule sets хранятся внутри системы. |
| `ACC-MVP-020` | Один project scan API создаёт `project_scans` и parent-child jobs workflow без отдельной сущности `security_scans`; security findings читаются через scoped или security endpoints. |
| `ACC-MVP-021` | Backend API покрывает scan roots, repositories, jobs, environments, workspaces и module states для Frontend MVP. |
| `ACC-MVP-022` | Clone workflow поддерживает `generic` Git и один managed provider из `gitlab` или `github`, выбор `https|ssh`, выбор root path и target directory, multi-host/multi-credential provider profiles, path safety и `job_locks`. |

### Platform acceptance

| ID | Criterion |
| --- | --- |
| `ACC-PLATFORM-001` | Поддерживаются все целевые SQL/SQL-like БД по roadmap: `PostgreSQL`, `SQLite`, `MySQL`, `MSSQL` и дополнительные SQL-compatible adapters, если они утверждены для platform release. |
| `ACC-PLATFORM-002` | Реализован полный administrative UI для auth, RBAC и `SCIM`. |
| `ACC-PLATFORM-003` | Реализован UI для редактирования configuration и security rule sets. |
| `ACC-PLATFORM-004` | Реализована full `.gitignore` semantics с отрицательными правилами `!pattern`. |
| `ACC-PLATFORM-005` | Подготовлен distributed deployment для `global-scanner`, `project-scanner`, `security-validator`, `repository-manager`, `auth`, `web`, `nginx` и БД. |
| `ACC-PLATFORM-006` | Repository integrations расширены до `gitlab`, `github`, `bitbucket`, `azure_devops`, recursive GitLab group clone, polling sync и webhook sync. |
| `ACC-PLATFORM-007` | Security adapters расширены до `Trivy`, `Gitleaks`, `Checkov`, `OPA` и `Conftest` с baseline локальными rule sets/policy packs. |
| `ACC-PLATFORM-008` | Полноценный SCIM sync workflow реализован поверх Stage 07 SCIM contract/stub. |

## Статус открытых решений

Канонический статус решений фиксируется в `docs/implementation-specs/stage-00-delivery-contract.md` в разделе `Decision register`. Если roadmap и Stage 00 расходятся, для управления внедрением используется Stage 00 decision register как более точный delivery contract.

- MVP breadth: accepted, MVP остаётся broad platform slice through Stage 08 и управляется stage ownership/test gates;
- bootstrap admin recovery model: accepted, строгая first-run recovery policy сохраняется без unauthenticated persistent recovery path;
- `terragrunt.hcl` support: deferred, не входит в MVP;
- baseline local security rules and policy packs: deferred до Stage 11 unless approved earlier;
- minimal external auth providers: deferred, Stage 07 поставляет local auth и provider interface;
- `project` / `environment` / `workspace` lifecycle: accepted for MVP в conservative mode;
- full `.gitignore` semantics: deferred до Stage 09, MVP matcher exclude-only с сохранением `!pattern`.
