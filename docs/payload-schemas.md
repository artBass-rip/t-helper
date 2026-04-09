# Payload schemas

Документ фиксирует минимальные JSON payload/result contracts для `jobs.payload`, `jobs.result_payload`, `project_scans.result_payload`, `module_states.details` и audit/config-related structured fields.

## Общие правила

- Каждый structured payload должен содержать `schema_version`.
- `schema_version` именуется как `<domain>.<operation>.v<major>`, например `jobs.global_scan.payload.v1`.
- Backward-compatible additions допускаются в рамках того же major version.
- Breaking changes требуют нового major version и миграции reader'ов.
- Payload не должен содержать секреты, tokens, приватные ключи или raw Terraform source.
- Поля `actor`, `correlation_id`, `idempotency_key` хранятся на уровне `jobs`, но могут дублироваться в payload только если это нужно для межмодульного сообщения.

## Jobs payloads

### `jobs.global_scan.payload.v1`

```json
{
  "schema_version": "jobs.global_scan.payload.v1",
  "root_path_ids": ["root_path_opaque_id"],
  "reason": "manual",
  "follow_symlinks": false,
  "ignore_rules_snapshot_id": "ignore_snapshot_opaque_id"
}
```

### `jobs.global_scan.result.v1`

```json
{
  "schema_version": "jobs.global_scan.result.v1",
  "root_path_ids": ["root_path_opaque_id"],
  "projects_created": 0,
  "projects_updated": 0,
  "repositories_created": 0,
  "repositories_updated": 0,
  "directories_skipped": 0,
  "errors_count": 0
}
```

### `jobs.project_scan.payload.v1`

```json
{
  "schema_version": "jobs.project_scan.payload.v1",
  "project_id": "project_opaque_id",
  "project_scan_id": "project_scan_opaque_id",
  "scan_type": "terraform_full",
  "rule_set_id": "rule_set_opaque_id",
  "reason": "manual"
}
```

### `jobs.project_scan.result.v1`

```json
{
  "schema_version": "jobs.project_scan.result.v1",
  "project_id": "project_opaque_id",
  "project_scan_id": "project_scan_opaque_id",
  "providers_count": 0,
  "findings_created": 0,
  "findings_updated": 0,
  "checks_succeeded": 0,
  "checks_failed": 0
}
```

### `jobs.security_validation_scan.payload.v1`

```json
{
  "schema_version": "jobs.security_validation_scan.payload.v1",
  "project_id": "project_opaque_id",
  "project_scan_id": "project_scan_opaque_id",
  "enabled_modules": ["trivy", "gitleaks", "checkov", "opa", "conftest"],
  "validate_code": true,
  "rule_set_id": "rule_set_opaque_id",
  "reason": "manual"
}
```

### `jobs.security_validation_scan.result.v1`

```json
{
  "schema_version": "jobs.security_validation_scan.result.v1",
  "project_id": "project_opaque_id",
  "project_scan_id": "project_scan_opaque_id",
  "modules_succeeded": 0,
  "modules_failed": 0,
  "findings_created": 0,
  "findings_updated": 0
}
```

### `jobs.repo_clone.payload.v1`

```json
{
  "schema_version": "jobs.repo_clone.payload.v1",
  "repository_id": "repository_opaque_id",
  "provider": "gitlab",
  "protocol": "ssh",
  "clone_scope": "single_repository",
  "clone_url": "ssh://git@example.local/group/repo.git",
  "group_path": null,
  "full_path": "group/repo",
  "root_path_id": "root_path_opaque_id",
  "new_root_path": null,
  "target_directory": "team-a/services",
  "new_target_directory": null,
  "reason": "manual"
}
```

### `jobs.repo_operation.result.v1`

Used by `repo_clone`, `repo_pull` and `repo_sync`.

```json
{
  "schema_version": "jobs.repo_operation.result.v1",
  "repository_id": "repository_opaque_id",
  "operation": "repo_clone",
  "root_path_id": "root_path_opaque_id",
  "provider": "gitlab",
  "protocol": "ssh",
  "local_path": "/srv/git-roots/team-a/group/repo",
  "repositories_created": 1,
  "before_revision": null,
  "after_revision": "git_revision",
  "changed": true
}
```

### `jobs.repo_pull.payload.v1`

```json
{
  "schema_version": "jobs.repo_pull.payload.v1",
  "repository_id": "repository_opaque_id",
  "reason": "manual"
}
```

### `jobs.repo_sync.payload.v1`

```json
{
  "schema_version": "jobs.repo_sync.payload.v1",
  "repository_id": "repository_opaque_id",
  "reason": "webhook"
}
```

### `jobs.config_reload.payload.v1`

```json
{
  "schema_version": "jobs.config_reload.payload.v1",
  "keys": ["scanning.global_scann", "logging.level"],
  "reason": "manual"
}
```

### `jobs.config_reload.result.v1`

```json
{
  "schema_version": "jobs.config_reload.result.v1",
  "applied_keys": ["logging.level"],
  "restart_required_keys": ["api.listen_address"],
  "failed_keys": []
}
```

### `jobs.module_restart.payload.v1`

```json
{
  "schema_version": "jobs.module_restart.payload.v1",
  "module_name": "global-scanner",
  "reason": "manual"
}
```

### `jobs.module_restart.result.v1`

```json
{
  "schema_version": "jobs.module_restart.result.v1",
  "module_name": "global-scanner",
  "previous_state": "running",
  "new_state": "running"
}
```

### `jobs.scim_sync.payload.v1`

```json
{
  "schema_version": "jobs.scim_sync.payload.v1",
  "provider": "local",
  "reason": "scheduled"
}
```

### `jobs.scim_sync.result.v1`

```json
{
  "schema_version": "jobs.scim_sync.result.v1",
  "provider": "local",
  "users_created": 0,
  "users_updated": 0,
  "groups_created": 0,
  "groups_updated": 0,
  "errors_count": 0
}
```

## Project scan result payload

`project_scans.result_payload` stores scan-specific summary. Detailed findings are stored in `security_findings`.

```json
{
  "schema_version": "project_scans.result.v1",
  "providers": [],
  "required_auth": [],
  "check_results": [],
  "findings_summary": {
    "info": 0,
    "low": 0,
    "medium": 0,
    "high": 0,
    "critical": 0
  }
}
```

## Module state details

```json
{
  "schema_version": "module_states.details.v1",
  "message": "running",
  "last_error": null,
  "last_transition_at": "2026-04-08T00:00:00Z"
}
```
