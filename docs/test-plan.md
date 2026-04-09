# Test plan

Документ связывает MVP acceptance criteria из [`roadmap.md`](roadmap.md) с проверками API, storage state и runtime behavior.

## Общие правила

- Acceptance tests должны запускаться против локального on-premise окружения без внешних SaaS dependencies.
- API assertions используют endpoint contracts из [`api.md`](api.md).
- Payload assertions используют schema versions из [`payload-schemas.md`](payload-schemas.md).
- Storage assertions используют сущности и инварианты из [`data-model.md`](data-model.md).
- Authorization assertions используют permissions из [`access-control.md`](access-control.md).
- Тестовые Terraform fixtures не должны содержать реальные секреты.

## MVP acceptance mapping

| ID | Acceptance | Primary checks |
| --- | --- | --- |
| `ACC-MVP-001` | Terraform-проект обнаруживается по наличию `*.tf`. | Создать fixture с `main.tf`; запустить `POST /api/scans`; проверить `projects` с `terraform_marker = *.tf`. |
| `ACC-MVP-002` | Глобальное сканирование определяет Terraform-проекты и Git-репозитории. | Fixture содержит `*.tf` и `.git`; проверить `projects`, `repositories`, `jobs.result_payload`. |
| `ACC-MVP-003` | После обнаружения проекта вложенные директории не сканируются как отдельные working directories. | Fixture содержит parent `main.tf` и nested `child/main.tf`; проверить отсутствие второго `project` под parent. |
| `ACC-MVP-004` | Ignore rules исключают файлы и директории; `!pattern` сохраняется. | Загрузить `.t-helper.ignore`; проверить `ignore_rules.pattern`, исключение directories и сохранение отрицательных patterns. |
| `ACC-MVP-005` | Проекты и репозитории сохраняются и обновляются в БД. | Запустить scan дважды; проверить отсутствие дублей по unique constraints и обновление `last_seen_at`. |
| `ACC-MVP-006` | Project-level scan определяет providers, required auth и quality через `terraform validate` и `TFLint`, а security/validation scan определяет validate results, secrets и security findings через `Trivy`, `Gitleaks`, `Checkov` и подключённые policy engines. | Настроить `PUT /api/projects/{id}/scan-settings`, запустить `POST /api/project-scans`; проверить `project_scans.result_payload`, `security_findings` и `jobs.job_type = security_validation_scan` для подключённых модулей `trivy`, `gitleaks`, `checkov`, `opa`, `conftest`. |
| `ACC-MVP-007` | Runtime-конфигурация хранится в БД. | Импортировать config; проверить `config_entries` и `GET /api/config`. |
| `ACC-MVP-008` | `thelper-ctl -reconfigure` импортирует конфигурацию и ignore rules. | Запустить CLI command; проверить `config_entries`, `ignore_rules`, отсутствие service restart side effect. |
| `ACC-MVP-009` | `thelper-ctl -reload` применяет reloadable-конфигурацию. | Изменить `logging.level` или `scanning.global_scann`; проверить `jobs.config_reload.result.v1`. |
| `ACC-MVP-010` | `thelper-ctl -restart <module>` работает для любого отдельного модуля. | Перезапустить `global-scanner`; проверить `module_states` transition и `jobs.module_restart.result.v1`. |
| `ACC-MVP-011` | `GUI` и `Web UI` используют единый backend API и покрывают MVP read/operate сценарии. | Контрактный тест: оба frontend clients используют только documented backend API. |
| `ACC-MVP-012` | `GUI` работает только локально. | Проверить bind policy/config и отказ удалённого GUI access. |
| `ACC-MVP-013` | `PostgreSQL` и `Badger` поддерживаются через storage abstraction. | Запустить один и тот же storage test suite против PostgreSQL и Badger adapters. |
| `ACC-MVP-014` | `clone`, `pull`, `sync` сериализуются через `job_locks`, clone использует общий root path, а новый root path автоматически попадает в root paths. | Запустить конкурирующие repo operations; проверить один `held` lock и отсутствие конфликтующих active jobs; выполнить `POST /api/repos/clone` с `new_root_path` и проверить появление нового `root_path`; проверить clone в существующую и новую `target_directory`. |
| `ACC-MVP-022` | Clone workflow поддерживает `gitlab`, `github`, `bitbucket`, выбор `https|ssh` и recursive clone GitLab group/subgroups. | Выполнить provider-specific clone requests; проверить корректный transport URL, `repositories.provider`, clone в выбранную `target_directory`; для `gitlab_group_recursive` проверить создание repository cards для проектов из группы и всех вложенных subgroup'ов. |
| `ACC-MVP-015` | Поддерживаются `environments` и `workspaces`. | Создать/импортировать связи; проверить `GET /api/environments`, `GET /api/workspaces` и FK constraints. |
| `ACC-MVP-016` | `auth` реализован как отдельный модуль. | Проверить `module_states` для `auth` и auth endpoints. |
| `ACC-MVP-017` | Базовые `SCIM` и `RBAC` контракты реализованы на backend/API уровне. | Проверить users/groups/roles/bindings/scim endpoints и negative authorization tests. |
| `ACC-MVP-018` | Security stack работает локально и не передаёт код наружу. | Запустить project scan в network-restricted окружении; проверить отсутствие outbound calls. |
| `ACC-MVP-019` | Security findings и rule sets хранятся внутри системы. | Проверить `security_rule_sets`, `security_findings`, `GET /api/security/findings`. |
| `ACC-MVP-020` | Один project scan API создаёт `project_scans`, findings читаются без `security_scans`. | Проверить `POST /api/project-scans`, `GET /api/project-scans/{project_scan_id}/findings`; убедиться, что отдельной сущности `security_scans` нет. |
| `ACC-MVP-021` | Backend API покрывает scan roots, repositories, jobs, environments, workspaces и module states для Frontend MVP. | Контрактный тест по endpoint list из `api.md` и authorization matrix. |

## Cross-cutting tests

### Idempotency

- Повторить write request с тем же `Idempotency-Key` и тем же payload.
- Ожидание: возвращается тот же `job_id` или тот же итоговый state.
- Повторить write request с тем же `Idempotency-Key`, но другим payload.
- Ожидание: `409 Conflict` или `validation_error` с явным кодом.

### Authorization

- Для каждого write endpoint проверить отказ без требуемого permission.
- Для object-scoped read проверить, что пользователь без object permission не получает sensitive details.
- Для `system.runtime.read` проверить только runtime summary/list metadata, без раскрытия object-scoped details.

### Payload schemas

- Для каждого `jobs.job_type` проверить наличие `schema_version` в `payload` и `result_payload`.
- Для `project_scans.result_payload` проверить `schema_version = project_scans.result.v1`.
- Проверить, что payload/result не содержит секреты, tokens, приватные ключи или raw Terraform source.

### Toolchain coverage

- Для `project-scanner` проверить, что runtime flow включает `terraform validate` и `TFLint`.
- Для `security-validator` проверить, что подключаемые модули `trivy`, `gitleaks`, `checkov` запускаются только при наличии в `project_security_scan_settings.enabled_modules`.
- Для enterprise-policy checks проверить, что `opa` и `conftest` могут быть зарегистрированы в `scanning.security_scan.modules` без изменения API contract.

### Storage invariants

- Проверить unique constraints из `data-model.md`.
- Проверить FK/delete behavior для `projects`, `workspaces`, `project_scans`, `job_locks`, `role_bindings` и `scim_identities`.
- Проверить, что expired `job_locks` не блокируют новые operations.

### Scanner fixtures

- `basic_tf_project`: директория с `main.tf`.
- `nested_tf_project`: parent с `main.tf` и nested `child/main.tf`.
- `git_repo_marker_directory`: `.git/` directory.
- `git_repo_marker_file`: `.git` file для worktree/submodule.
- `ignored_directory`: директория, исключённая `.t-helper.ignore`.
- `negative_ignore_pattern`: rule `!pattern`, сохранённая без применения в exclude-only MVP matcher.
