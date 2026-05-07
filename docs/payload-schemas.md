# Payload schemas

Документ фиксирует минимальные JSON payload/result contracts для `jobs.payload`, `jobs.result_payload`, `project_scans.result_payload`, `module_states.details` и audit/config-related structured fields.

## Общие правила

- Каждый structured payload должен содержать `schema_version`.
- `schema_version` именуется как `<domain>.<operation>.v<major>`, например `jobs.global_scan.payload.v1`.
- Backward-compatible additions допускаются в рамках того же major version.
- Breaking changes требуют нового major version и миграции reader'ов.
- Payload не должен содержать секреты, tokens, приватные ключи или raw Terraform source.
- Payload не должен содержать raw passwords, password hashes, password reset tokens или reset token hashes.
- Поля `actor`, `correlation_id`, `idempotency_key` хранятся на уровне `jobs`, но могут дублироваться в payload только если это нужно для межмодульного сообщения.
- Worker execution metadata хранится на уровне `jobs` (`leased_by`, `attempt_count`, `lease_expires_at`, `heartbeat_at`, `run_after`) и может дублироваться в `result_payload` только для диагностики без секретов.
- Result payload для failed jobs должен использовать `jobs.failure.result.v1`, если job-specific failed result schema явно не описана.
- Jobs должны публиковать status events/metrics в `job_events`; aggregate workflow status хранится в `workflow_statuses`.
  Runtime heartbeat freshness is stored on `jobs.heartbeat_at`; heartbeat
  events are bounded diagnostics and are not required for every heartbeat tick.

## Jobs payloads

### `jobs.global_scan.payload.v1`

MVP supports only `follow_symlinks = false`. `true` is reserved for a future hardening stage and must not be accepted unless that capability is explicitly implemented.

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
  "projects_marked_missing": 0,
  "project_discovery_jobs_enqueued": 0,
  "directories_skipped": 0,
  "symlinks_skipped": 0,
  "errors_count": 0
}
```

### `jobs.project_discovery.payload.v1`

```json
{
  "schema_version": "jobs.project_discovery.payload.v1",
  "project_id": "project_opaque_id",
  "root_path_id": "root_path_opaque_id",
  "relative_path": "team/service",
  "reason": "global_scan"
}
```

### `jobs.project_discovery.result.v1`

```json
{
  "schema_version": "jobs.project_discovery.result.v1",
  "project_id": "project_opaque_id",
  "git_repository_detected": true,
  "repository_id": "repository_opaque_id",
  "repository_created": false,
  "repository_updated": true,
  "linked_project_ids": ["project_other_opaque_id"],
  "links_created": 1,
  "git_marker_type": ".git_directory"
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
  "reason": "manual",
  "security_validation_requested": true
}
```

### `jobs.project_scan.result.v1`

```json
{
  "schema_version": "jobs.project_scan.result.v1",
  "project_id": "project_opaque_id",
  "project_scan_id": "project_scan_opaque_id",
  "tools": [
    {
      "tool": "terraform",
      "tool_version": "1.8.5",
      "profile_id": "terraform-validate-json-v1",
      "profile_version": "1.0.0",
      "compatibility_status": "certified",
      "certification_status": "certified"
    }
  ],
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
  "reason": "manual",
  "parent_job_id": "job_parent_opaque_id"
}
```

### `jobs.security_validation_scan.result.v1`

```json
{
  "schema_version": "jobs.security_validation_scan.result.v1",
  "project_id": "project_opaque_id",
  "project_scan_id": "project_scan_opaque_id",
  "tools": [
    {
      "tool": "trivy",
      "tool_version": "0.60.0",
      "profile_id": "trivy-terraform-misconfig-json-v1",
      "profile_version": "1.0.0",
      "compatibility_status": "certified",
      "certification_status": "certified"
    }
  ],
  "modules_succeeded": 0,
  "modules_failed": 0,
  "findings_created": 0,
  "findings_updated": 0
}
```

Tool result metadata records the discovered tool version and selected profile
without storing raw tool output. `compatibility_status` accepts `certified`,
`compatible`, `unsupported` or `unknown`. `certification_status` accepts
`certified` or `uncertified`.

### `jobs.repo_clone.payload.v1`

`clone_url` is optional transport metadata. It must be safe-normalized and must not contain credentials, tokens, passwords or other userinfo. Repository identity is resolved from `provider`, `provider_host` and `full_path`; provider URL parsing follows ADR 0016.

```json
{
  "schema_version": "jobs.repo_clone.payload.v1",
  "repository_id": "repository_opaque_id",
  "provider_instance_id": "provider_instance_opaque_id",
  "credential_id": "credential_opaque_id",
  "provider": "gitlab",
  "provider_host": "git.example.local",
  "protocol": "ssh",
  "clone_scope": "single_repository",
  "clone_url": "ssh://git.example.local/group/repo.git",
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

`local_path` is persisted result metadata. Result payloads must not include credential-bearing clone URLs.

```json
{
  "schema_version": "jobs.repo_operation.result.v1",
  "repository_id": "repository_opaque_id",
  "provider_instance_id": "provider_instance_opaque_id",
  "credential_id": "credential_opaque_id",
  "operation": "repo_clone",
  "root_path_id": "root_path_opaque_id",
  "provider": "gitlab",
  "provider_host": "git.example.local",
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
  "credential_id": "credential_opaque_id",
  "reason": "manual"
}
```

### `jobs.repo_sync.payload.v1`

```json
{
  "schema_version": "jobs.repo_sync.payload.v1",
  "repository_id": "repository_opaque_id",
  "credential_id": "credential_opaque_id",
  "reason": "webhook"
}
```

### `jobs.config_reload.payload.v1`

Stage 03 job-backed config reload payload. Stage 02 uses the synchronous
`config_reload.result.v1` response shape below without creating a `jobs` row.

```json
{
  "schema_version": "jobs.config_reload.payload.v1",
  "keys": ["scanning.global_scan", "logging.level"],
  "reason": "manual"
}
```

### `jobs.config_reload.result.v1`

```json
{
  "schema_version": "jobs.config_reload.result.v1",
  "accepted_keys": ["logging.level"],
  "applied_keys": [],
  "restart_required_keys": ["api.listen_address"],
  "failed_keys": []
}
```

### `jobs.failure.result.v1`

Default failed job result payload. It is used unless a job-specific failed
result schema is explicitly documented.

```json
{
  "schema_version": "jobs.failure.result.v1",
  "job_type": "module_restart",
  "worker_id": "host:12345:worker_uuid",
  "attempt": 1,
  "error_code": "validation_error",
  "message": "module_name is required",
  "retryable": false
}
```

`error_code` is machine-readable. Stage 03 baseline values are
`validation_error`, `unknown_job_type`, `lock_contention`, `transient_error`,
`handler_failed` and `cancelled`.

### `config_import.result.v1`

Stage 02 synchronous config import result.

```json
{
  "schema_version": "config_import.result.v1",
  "applied_keys": ["logging.level", "modules.enabled"],
  "restart_required_keys": ["api.listen_address"],
  "ignore_rules": [
    {
      "scope_type": "system",
      "pattern": ".terraform/",
      "origin": "config_import"
    }
  ]
}
```

### `config_reload.result.v1`

Stage 02 synchronous config reload result.

```json
{
  "schema_version": "config_reload.result.v1",
  "accepted_keys": ["logging.level", "modules.enabled"],
  "applied_keys": ["modules.enabled"],
  "restart_required_keys": ["api.listen_address"],
  "failed_keys": []
}
```

For an explicit request with unknown keys:

```json
{
  "schema_version": "config_reload.result.v1",
  "accepted_keys": [],
  "applied_keys": [],
  "restart_required_keys": [],
  "failed_keys": ["logging.levl"]
}
```

### `jobs.module_restart.payload.v1`

Stage 03 job-backed module restart payload. Stage 02 uses synchronous
`module_restart.result.v1` and `module_reload.result.v1` response shapes without
creating a `jobs` row.

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

### `module_restart.result.v1`

Stage 02 synchronous module restart result.

```json
{
  "schema_version": "module_restart.result.v1",
  "module_name": "config-manager",
  "previous_state": "running",
  "new_state": "running"
}
```

### `module_reload.result.v1`

Stage 02 synchronous module reload result.

```json
{
  "schema_version": "module_reload.result.v1",
  "module_name": "core",
  "previous_state": "running",
  "new_state": "running"
}
```

### `storage_migration.result.v1`

Stage 02 synchronous controlled storage migration result.

```json
{
  "schema_version": "storage_migration.result.v1",
  "status": "migration_succeeded",
  "previous_current_profile_id": "storage_profile_opaque_id",
  "new_current_profile_id": "storage_profile_opaque_id",
  "current_profile_unchanged": false
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
  "job_group_id": "project_scan:project_scan_opaque_id",
  "parent_job_id": "job_parent_opaque_id",
  "child_job_ids": ["job_child_opaque_id"],
  "tools": [],
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

## Security finding fingerprint

`security_findings.fingerprint_components` stores the canonical JSON input for ADR 0017 fingerprint generation. The stored `fingerprint` is `fp:v1:<sha256 canonical_json>`.

```json
{
  "schema_version": "security_finding.fingerprint.v1",
  "project_id": "project_opaque_id",
  "workspace_id": null,
  "rule_set_id": "rule_set_opaque_id",
  "rule_namespace": "trivy",
  "tool": "trivy",
  "check_type": "terraform.security.misconfig",
  "rule_id": "AVD-AWS-0088",
  "normalized_file_path": "modules/s3/main.tf",
  "resource_ref": "aws_s3_bucket.example",
  "finding_key": "aws_s3_bucket.example:public-read"
}
```

## Status schemas

HTTP response DTOs for `runtime_status.v1`, `job_status.v1` and
`worker_status.v1` are defined in [`api.md`](api.md). This section defines the
structured payloads embedded in status storage/read models.

### `job_events.payload.v1`

```json
{
  "schema_version": "job_events.payload.v1",
  "message": "trivy module completed",
  "details": {}
}
```

### `workflow_status.summary.v1`

Generic workflow summary payload owned by `status-monitor`. Domain-specific
components may be added under `components`, but the baseline `counts` and
`latest_event` fields remain stable for all workflow types.

```json
{
  "schema_version": "workflow_status.summary.v1",
  "workflow_type": "module_operation",
  "workflow_id": "job_opaque_id",
  "job_group_id": "module_operation:job_opaque_id",
  "aggregate_status": "running",
  "counts": {
    "queued": 0,
    "running": 1,
    "succeeded": 0,
    "failed": 0,
    "cancelled": 0
  },
  "latest_event": {
    "job_id": "job_opaque_id",
    "event_type": "progress",
    "status": "running",
    "worker_id": "host:12345:worker_uuid",
    "created_at": "2026-04-08T00:00:00Z"
  },
  "components": {
    "module_operation": {
      "module_name": "config-manager",
      "operation": "module_restart"
    }
  }
}
```

If no `job_events` exist for the workflow, `latest_event` is `null`. Heartbeat
freshness is read from `jobs.heartbeat_at`; `latest_event` may be a lifecycle,
progress or diagnostic heartbeat event.

## Module state details

```json
{
  "schema_version": "module_states.details.v1",
  "message": "running",
  "last_error": null,
  "last_transition_at": "2026-04-08T00:00:00Z"
}
```
