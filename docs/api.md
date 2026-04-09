# Backend API skeleton

Документ фиксирует минимальный HTTP API contract для MVP scaffolding. Канонический список endpoint'ов остаётся в [`interfaces.md`](interfaces.md), а правила авторизации - в [`access-control.md`](access-control.md).

## Общие правила

- Base path: `/api`
- Формат тела запроса и ответа: JSON.
- Идентификаторы в URL opaque для API consumers.
- Время возвращается в UTC в RFC 3339 format.
- Write endpoints, которые запускают фоновые операции, возвращают `202 Accepted` и `job_id`.
- Write endpoints, которые меняют справочники или конфигурацию без фоновой операции, возвращают обновлённую сущность или `204 No Content`.
- Повторяемые write requests должны поддерживать `Idempotency-Key`, если операция создаёт `job`.
- List endpoints должны поддерживать `limit` и `cursor`; фильтры добавляются по capability.
- Ошибки возвращаются в едином формате `api_error`.

## Общие response schemas

### `job_ref`

```json
{
  "job_id": "job_opaque_id",
  "status": "queued",
  "schema_version": "job_ref.v1"
}
```

### `project_scan_ref`

```json
{
  "project_scan_id": "project_scan_opaque_id",
  "job_id": "job_opaque_id",
  "status": "queued",
  "schema_version": "project_scan_ref.v1"
}
```

### `api_error`

```json
{
  "error": {
    "code": "validation_error",
    "message": "human-readable message",
    "details": {},
    "correlation_id": "request_or_job_correlation_id"
  }
}
```

### `list_response`

```json
{
  "items": [],
  "next_cursor": null
}
```

## Endpoint skeleton

| Endpoint | Request | Success response | Notes |
| --- | --- | --- | --- |
| `GET /api/root-paths` | `limit`, `cursor` | `list_response<root_path>` | Возвращает активные и отключённые root paths. |
| `PUT /api/root-paths` | `root_paths[]` | `list_response<root_path>` | Идемпотентно заменяет набор root paths. |
| `POST /api/scans` | `root_path_ids?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = global_scan`; `job_ref.job_id` используется для чтения статуса через `GET /api/scans/{job_id}` или `GET /api/jobs/{job_id}`. |
| `GET /api/scans/{job_id}` | n/a | `job` | Compatibility endpoint для global scan jobs; canonical job status endpoint - `GET /api/jobs/{id}`. |
| `GET /api/projects` | `limit`, `cursor`, `root_path_id?`, `repository_id?`, `status?` | `list_response<project>` | Read model для registry. |
| `GET /api/projects/{id}` | n/a | `project` | Возвращает проект с базовыми связями. |
| `GET /api/projects/{id}/scan-settings` | n/a | `project_scan_settings` | Возвращает project-level и security/validation scan settings для проекта. |
| `PUT /api/projects/{id}/scan-settings` | `project_scan_settings` | `project_scan_settings` | Идемпотентно обновляет scan settings проекта; `security.enabled_modules` должны входить в `scanning.security_scan.modules`. |
| `POST /api/project-scans` | `project_id`, `scan_type?`, `rule_set_id?`, `reason?` | `202 project_scan_ref` | Создаёт `jobs.job_type = project_scan` и `project_scans`. |
| `GET /api/project-scans/{project_scan_id}` | n/a | `project_scan` | `project_scan_id` ссылается на `project_scans.id`; связанный job доступен через `project_scan.job_id`. |
| `GET /api/project-scans/{project_scan_id}/findings` | `limit`, `cursor`, `severity?`, `status?` | `list_response<security_finding>` | Scoped findings для одного project scan. |
| `GET /api/repos` | `limit`, `cursor`, `full_path?`, `auto_sync_enabled?` | `list_response<repository>` | Read model для repositories. |
| `GET /api/repos/{id}` | n/a | `repository` | Возвращает repository card. |
| `POST /api/repos/clone` | `provider`, `protocol`, `clone_url?`, `group_path?`, `clone_scope`, `full_path?`, `root_path_id?`, `new_root_path?`, `target_directory?`, `new_target_directory?` | `202 job_ref` | Создаёт `jobs.job_type = repo_clone`; clone выполняется в существующий `root_path` или в новый path, который затем добавляется в `root_paths`; target directory выбирается внутри root path; поддерживаются provider-aware clone flows для `gitlab`, `github`, `bitbucket`; для `gitlab_group_recursive` клонируются все проекты группы и всех вложенных subgroup'ов. |
| `POST /api/repos/pull` | `repository_id` | `202 job_ref` | Создаёт `jobs.job_type = repo_pull`. |
| `POST /api/repos/sync` | `repository_id`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = repo_sync`. |
| `GET /api/jobs` | `limit`, `cursor`, `job_type?`, `status?`, `lock_key?` | `list_response<job>` | Общая видимость jobs. |
| `GET /api/jobs/{id}` | n/a | `job` | Возвращает payload/result metadata без секретов. |
| `GET /api/config` | n/a | `config` | Возвращает активную runtime-конфигурацию с masked secrets. |
| `PUT /api/config` | `config` | `config` or `202 job_ref` | Возвращает `job_ref`, если требуется reload/restart workflow. |
| `GET /api/ignore-rules` | `limit`, `cursor`, `scope_type?`, `scope_id?` | `list_response<ignore_rule>` | Возвращает правила без потери `!pattern`. |
| `PUT /api/ignore-rules` | `ignore_rules[]` | `list_response<ignore_rule>` | Идемпотентно заменяет правила в указанном scope. |
| `GET /api/security/findings` | `limit`, `cursor`, `project_id?`, `repository_id?`, `severity?`, `status?` | `list_response<security_finding>` | Global findings view. |
| `GET /api/security/findings/{id}` | n/a | `security_finding` | Детальная карточка finding. |
| `GET /api/security/rule-sets` | `limit`, `cursor`, `active?` | `list_response<security_rule_set>` | Список rule sets. |
| `PUT /api/security/rule-sets` | `security_rule_set` | `security_rule_set` | Идемпотентное обновление metadata/rule set registration. |
| `GET /api/modules` | n/a | `list_response<module_state>` | Состояния runtime modules. |
| `POST /api/modules/reload` | `keys?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = config_reload`. |
| `POST /api/modules/restart` | `module_name`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = module_restart`. |
| `GET /api/environments` | `limit`, `cursor` | `list_response<environment>` | MVP read endpoint. |
| `GET /api/environments/{id}` | n/a | `environment` | MVP read endpoint. |
| `GET /api/workspaces` | `limit`, `cursor`, `project_id?`, `environment_id?` | `list_response<workspace>` | MVP read endpoint. |
| `GET /api/workspaces/{id}` | n/a | `workspace` | MVP read endpoint. |
| `GET /api/auth/users` | `limit`, `cursor`, `active?` | `list_response<user>` | Administrative endpoint; UI may be hardening scope. |
| `PUT /api/auth/users` | `users[]` | `list_response<user>` | Идемпотентное обновление users. |
| `GET /api/auth/groups` | `limit`, `cursor` | `list_response<group>` | Administrative endpoint; UI may be hardening scope. |
| `PUT /api/auth/groups` | `groups[]` | `list_response<group>` | Идемпотентное обновление groups. |
| `GET /api/auth/roles` | `limit`, `cursor`, `scope_type?` | `list_response<role>` | Administrative endpoint; UI may be hardening scope. |
| `PUT /api/auth/roles` | `roles[]` | `list_response<role>` | Идемпотентное обновление roles. |
| `GET /api/auth/role-bindings` | `limit`, `cursor`, `scope_type?`, `scope_id?`, `subject_type?`, `subject_id?` | `list_response<role_binding>` | Administrative endpoint; UI may be hardening scope. |
| `PUT /api/auth/role-bindings` | `role_bindings[]` | `list_response<role_binding>` | Идемпотентное обновление bindings. |
| `GET /api/auth/scim/identities` | `limit`, `cursor`, `provider?` | `list_response<scim_identity>` | SCIM identity read model. |
| `POST /api/auth/scim/sync` | `provider?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = scim_sync`. |
| `GET /api/audit` | `limit`, `cursor`, `actor?`, `entity_type?`, `action?` | `list_response<audit_log>` | Audit log read model. |

## Schema sources

Имена сущностей в таблице endpoint'ов соответствуют persistent entities из [`data-model.md`](data-model.md). API DTO могут скрывать внутренние поля, но имена полей должны оставаться согласованными, если нет явно описанной причины для расхождения.

Версионируемые `jobs.payload`, `jobs.result_payload`, `project_scans.result_payload` и `module_states.details` описаны в [`payload-schemas.md`](payload-schemas.md).
