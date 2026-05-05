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
- Confirmed MVP behavior: bulk `PUT` endpoints are non-destructive idempotent upserts by stable identity or `id`; omitted records are not deleted unless a future endpoint explicitly documents delete/disable semantics.
- Confirmed MVP behavior: public `DELETE` endpoints are out of scope; delete permissions are seeded for future lifecycle expansion and do not imply an implemented delete route.
- Confirmed MVP lifecycle behavior: user-facing removal/deactivation is expressed through explicit state fields and non-destructive `PUT` updates, for example `enabled = false`, `active = false`, `status = disabled`, `status = missing` or `status = superseded` depending on entity type. UI/API wording must use disable, deactivate, mark missing or supersede instead of delete unless a future endpoint explicitly defines hard delete semantics.
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
  "job_group_id": "project_scan:project_scan_opaque_id",
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

### `health_status`

Confirmed MVP behavior: `GET /api/health` is intentionally safe for unauthenticated local discovery. It must not expose config values, filesystem paths, DSNs, users, secrets or object-scoped details.

```json
{
  "instance_id": "runtime_instance_opaque_id",
  "mode": "local",
  "database_fingerprint": "db_fingerprint",
  "started_at": "2026-04-08T00:00:00Z",
  "readiness": "ready",
  "schema_version": "health_status.v1"
}
```

### `auth_session`

```json
{
  "session_id": "session_opaque_id",
  "user": {
    "id": "user_opaque_id",
    "username": "admin",
    "display_name": "Administrator"
  },
  "expires_at": "2026-04-08T12:00:00Z",
  "schema_version": "auth_session.v1"
}
```

## Endpoint skeleton

| Endpoint | Request | Success response | Notes |
| --- | --- | --- | --- |
| `GET /api/health` | n/a | `health_status` | Safe unauthenticated health/readiness endpoint for singleton runtime discovery; detailed status remains under authenticated `/api/status`. |
| `GET /api/root-paths` | `limit`, `cursor` | `list_response<root_path>` | Возвращает активные и отключённые root paths. |
| `PUT /api/root-paths` | `root_paths[]` | `list_response<root_path>` | Идемпотентно upsert'ит root paths by `id` or normalized `path`; omitted records are not deleted in MVP. |
| `POST /api/scans` | `root_path_ids?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = global_scan`; global scan creates/updates project records and enqueues background `project_discovery` jobs without waiting for them; `job_ref.job_id` используется для чтения статуса через `GET /api/scans/{job_id}` или `GET /api/jobs/{job_id}`. |
| `GET /api/scans/{job_id}` | n/a | `job` | Temporary compatibility endpoint для global scan jobs; canonical job status endpoint - `GET /api/jobs/{id}`. Frontend MUST use `GET /api/jobs/{id}` or status endpoints for new code. Compatibility endpoint remains during MVP and may be removed only after one documented deprecation cycle. |
| `GET /api/projects` | `limit`, `cursor`, `root_path_id?`, `repository_id?`, `status?` | `list_response<project>` | Read model для registry. Default `status` filter is `active`; use `status=missing`, `status=disabled` or `status=all` to include non-active projects. |
| `GET /api/projects/{id}` | n/a | `project` | Возвращает проект с базовыми связями. |
| `GET /api/projects/{id}/scan-settings` | n/a | `project_scan_settings` | Возвращает project-level и security/validation scan settings для проекта. |
| `PUT /api/projects/{id}/scan-settings` | `project_scan_settings` | `project_scan_settings` | Идемпотентно обновляет scan settings проекта; `security.enabled_modules` должны входить в `scanning.security_scan.modules`. |
| `POST /api/project-scans` | `project_id`, `scan_type?`, `rule_set_id?`, `reason?` | `202 project_scan_ref` | Создаёт `project_scans`, parent `jobs.job_type = project_scan` и `job_group_id = project_scan:<project_scan_id>`; parent job создаёт child `security_validation_scan` jobs, если security modules включены. |
| `GET /api/project-scans/{project_scan_id}` | n/a | `project_scan` | Возвращает aggregate status/read model из `status-monitor`; `project_scan.job_id` указывает на parent job, `job_group_id` связывает parent/child jobs. |
| `GET /api/project-scans/{project_scan_id}/findings` | `limit`, `cursor`, `severity?`, `status?` | `list_response<security_finding>` | Scoped findings для одного project scan. |
| `GET /api/repos` | `limit`, `cursor`, `provider?`, `provider_host?`, `full_path?`, `status?`, `discovery_source?`, `auto_sync_enabled?` | `list_response<repository>` | Read model для repositories. Default `status` filter is `active`; `full_path` без provider/provider_host может вернуть несколько repositories. |
| `GET /api/repos/{id}` | n/a | `repository` | Возвращает repository card. |
| `GET /api/repo-provider-instances` | `limit`, `cursor`, `provider?`, `provider_host?`, `enabled?` | `list_response<repository_provider_instance>` | GitKraken-like integration profiles for cloud, on-premise and multi-domain provider hosts. |
| `PUT /api/repo-provider-instances` | `repository_provider_instances[]` | `list_response<repository_provider_instance>` | Идемпотентно upsert'ит configured provider hosts/profiles by `id` or `provider + provider_host`; secrets are not accepted here. |
| `GET /api/repo-credentials` | `limit`, `cursor`, `provider_instance_id?`, `usage?`, `auth_type?` | `list_response<repository_credential>` | Возвращает credentials with masked secret refs only. |
| `PUT /api/repo-credentials` | `repository_credentials[]` | `list_response<repository_credential>` | Идемпотентно upsert'ит credential metadata and secret refs by `id` or `provider_instance_id + name`; raw secret values are rejected. |
| `POST /api/repos/clone` | `provider_instance_id?`, `provider`, `provider_host?`, `credential_id?`, `protocol`, `clone_url?`, `group_path?`, `clone_scope`, `full_path?`, `root_path_id?`, `new_root_path?`, `target_directory?`, `new_target_directory?` | `202 job_ref` | Создаёт `jobs.job_type = repo_clone`; URL parsing follows ADR 0016; repository identity нормализуется как `provider + provider_host + full_path`; before a stable `repository_id` exists, clone checks active conflicts by normalized repository identity and normalized target path; `provider_instance_id` выбирает configured host/profile; `credential_id` выбирает credential with required usage; `clone_url` принимается только как transport metadata, не участвует в deduplication и не должен сохраняться с credentials/userinfo; Stage 05 MVP supports `single_repository` clone for `generic` Git and one managed provider from `gitlab`/`github`; `gitlab_group_recursive` is a post-MVP extension and requires `provider_api` credential usage when enabled. |
| `POST /api/repos/pull` | `repository_id`, `credential_id?` | `202 job_ref` | Создаёт `jobs.job_type = repo_pull`; credential must support `git_transport`. |
| `POST /api/repos/sync` | `repository_id`, `credential_id?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = repo_sync`; credential usage depends on sync mode. |
| `GET /api/jobs` | `limit`, `cursor`, `job_type?`, `status?`, `lock_key?`, `job_group_id?`, `parent_job_id?` | `list_response<job>` | Общая видимость jobs. Для UI workflow status предпочтительнее status/project-scan aggregate endpoints. |
| `GET /api/jobs/{id}` | n/a | `job` | Возвращает payload/result metadata без секретов. |
| `GET /api/status` | n/a | `runtime_status` | Aggregate runtime status из `status-monitor`. |
| `GET /api/status/workflows` | `limit`, `cursor`, `workflow_type?`, `aggregate_status?` | `list_response<workflow_status>` | Aggregate workflow statuses. |
| `GET /api/status/workflows/{job_group_id}` | n/a | `workflow_status` | Единая точка чтения workflow status по `job_group_id`. |
| `GET /api/status/jobs/{job_id}` | n/a | `job_status` | Aggregate job status, latest event и диагностические metadata. |
| `GET /api/status/workers` | n/a | `list_response<worker_status>` | Worker health/status из status aggregation layer. |
| `GET /api/config` | n/a | `config` | Возвращает активную runtime-конфигурацию с masked sensitive values; resolved secrets never returned. |
| `PUT /api/config` | `config` | `config` or `202 job_ref` | Использует strict schema validation, rejects unknown keys and sensitive literals; возвращает `job_ref`, если требуется reload/restart workflow. |
| `GET /api/ignore-rules` | `limit`, `cursor`, `scope_type?`, `scope_id?` | `list_response<ignore_rule>` | Возвращает правила без потери `!pattern`. |
| `PUT /api/ignore-rules` | `ignore_rules[]` | `list_response<ignore_rule>` | Идемпотентно upsert'ит rules by `scope_type + scope_id + pattern`; omitted rules are not deleted in MVP. |
| `GET /api/security/findings` | `limit`, `cursor`, `project_id?`, `repository_id?`, `severity?`, `status?` | `list_response<security_finding>` | Global findings view. |
| `GET /api/security/findings/{id}` | n/a | `security_finding` | Детальная карточка finding. |
| `GET /api/security/rule-sets` | `limit`, `cursor`, `active?` | `list_response<security_rule_set>` | Список rule sets. |
| `PUT /api/security/rule-sets` | `security_rule_set` | `security_rule_set` | Идемпотентное обновление metadata/rule set registration by `id` or `name + version`. |
| `GET /api/tool-profiles` | `limit`, `cursor`, `tool?`, `active?`, `source_type?` | `list_response<tool_profile>` | Список tool profiles from ADR 0018. |
| `POST /api/tool-profiles/validate` | `profile_path?`, `profile_payload?`, `fixture_set?` | `tool_profile_validation_result` | Валидирует profile files or payloads without activation. Raw tool outputs in fixtures must be redacted/size-limited and are not persisted as primary scan data. |
| `POST /api/tool-profiles/import` | `profile_path?`, `profile_payload?` | `tool_profile` | Imports bundled/local/generated profile metadata after validation; imported profiles are inactive unless separately activated. |
| `POST /api/tool-profiles/activate` | `tool`, `profile_id`, `profile_version` | `tool_profile` | Explicitly activates a validated profile for runtime selection. Generated candidate profiles cannot be activated without successful validation results. |
| `POST /api/tool-profiles/analyze` | `samples_path?`, `sample_payload?`, `baseline_profile_id?` | `tool_profile_candidate` | Optional analyzer endpoint that generates candidate profiles/fixtures; it never activates profiles automatically. |
| `GET /api/modules` | n/a | `list_response<module_state>` | Состояния runtime modules. |
| `POST /api/modules/reload` | `keys?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = config_reload`. |
| `POST /api/modules/restart` | `module_name`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = module_restart`. |
| `GET /api/environments` | `limit`, `cursor` | `list_response<environment>` | MVP read endpoint. |
| `GET /api/environments/{id}` | n/a | `environment` | MVP read endpoint. |
| `GET /api/workspaces` | `limit`, `cursor`, `project_id?`, `environment_id?` | `list_response<workspace>` | MVP read endpoint. |
| `GET /api/workspaces/{id}` | n/a | `workspace` | MVP read endpoint. |
| `POST /api/auth/login` | `username`, `password` | `auth_session` | Local auth login; failures use generic errors and must not reveal whether username exists. |
| `POST /api/auth/logout` | n/a | `204 No Content` | Invalidates current session/token. |
| `GET /api/auth/session` | n/a | `auth_session` | Returns current authenticated session and safe user metadata. |
| `POST /api/auth/password-reset/request` | `username_or_email` | `204 No Content` | Starts reset flow with generic success response; raw reset token must never be stored. |
| `POST /api/auth/password-reset/confirm` | `token`, `new_password` | `204 No Content` | Verifies one-time reset token and updates local credentials using Argon2id PHC hashing. |
| `POST /api/auth/password/change` | `current_password`, `new_password` | `204 No Content` | Changes password for current authenticated local user. |
| `GET /api/auth/users` | `limit`, `cursor`, `active?` | `list_response<user>` | Administrative endpoint; full MVP UI is Stage 08 scope. |
| `PUT /api/auth/users` | `users[]` | `list_response<user>` | Идемпотентно upsert'ит users by `id` or `username`; omitted users are not deleted in MVP. |
| `GET /api/auth/groups` | `limit`, `cursor` | `list_response<group>` | Administrative endpoint; full MVP UI is Stage 08 scope. |
| `PUT /api/auth/groups` | `groups[]` | `list_response<group>` | Идемпотентно upsert'ит groups by `id` or `name`; omitted groups are not deleted in MVP. |
| `GET /api/auth/roles` | `limit`, `cursor`, `scope_type?` | `list_response<role>` | Administrative endpoint; full MVP UI is Stage 08 scope. |
| `PUT /api/auth/roles` | `roles[]` | `list_response<role>` | Идемпотентно upsert'ит roles by `id` or `name + scope_type`; omitted roles are not deleted in MVP. |
| `GET /api/auth/role-bindings` | `limit`, `cursor`, `scope_type?`, `scope_id?`, `subject_type?`, `subject_id?` | `list_response<role_binding>` | Administrative endpoint; full MVP UI is Stage 08 scope. |
| `PUT /api/auth/role-bindings` | `role_bindings[]` | `list_response<role_binding>` | Идемпотентно upsert'ит bindings by subject/role/scope tuple; omitted bindings are not deleted in MVP. |
| `GET /api/auth/scim/identities` | `limit`, `cursor`, `provider?` | `list_response<scim_identity>` | SCIM identity read model. |
| `POST /api/auth/scim/sync` | `provider?`, `reason?` | `202 job_ref` | Создаёт `jobs.job_type = scim_sync`. |
| `GET /api/audit` | `limit`, `cursor`, `actor?`, `entity_type?`, `action?` | `list_response<audit_log>` | Audit log read model. |

## Repository operation conflicts

For `clone`, `pull` and `sync`, if another `queued` or `running` repository
operation exists for the same `lock_key = repository:<repository_id>`, a new
request returns `409 conflict` with code
`repository_operation_already_running`, except exact `Idempotency-Key` replay,
which returns the existing `job_ref`.

Clone also checks pre-create conflicts by normalized repository identity and
normalized target path before a stable `repository_id` exists.

## Schema sources

Имена сущностей в таблице endpoint'ов соответствуют persistent entities из [`data-model.md`](data-model.md). API DTO могут скрывать внутренние поля, но имена полей должны оставаться согласованными, если нет явно описанной причины для расхождения.

Версионируемые `jobs.payload`, `jobs.result_payload`, `project_scans.result_payload` и `module_states.details` описаны в [`payload-schemas.md`](payload-schemas.md).
