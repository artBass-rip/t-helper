# Data Model

This document describes the target persistent data model. It does not mean that all
tables must appear in the first migration.

Physical migrations are stage-owned: a table or index is introduced only by the
stage that also ships the code, API or worker behavior and tests for its
invariants. Stage 01 must not create the whole target schema in advance.

## Stage-Owned Migrations

Expected migration ownership:

- Stage 01: migration metadata, storage bootstrap metadata, health/schema
  version support and only the minimal system tables required by the backend
  skeleton.
- Stage 02: `config_entries`, `storage_profiles`,
  `storage_provider_settings`, `module_states`, imported system
  `ignore_rules` and runtime lifecycle state.
- Stage 03: `jobs`, `job_locks`, `job_events`, `workflow_statuses`.
- Stage 04: `root_paths`, project-scoped/runtime scanner `ignore_rules`
  behavior, `projects`, `project_links`, minimal `repositories`,
  `environments`, `workspaces`.
- Stage 05: full repository model, `repository_provider_instances`,
  `repository_credentials`, repository enrichment/supersede fields and
  repository-operation indexes.
- Stage 06A: `tool_profiles`, `tool_profile_validation_results`.
- Stage 06B: `project_scan_settings`, `project_security_scan_settings`,
  `project_scans`, `security_rule_sets`, `security_findings`.
- Stage 07: `users`, `local_user_credentials`, `password_reset_tokens`,
  `user_sessions`, `auth_bootstrap_credentials`, `groups`, `group_members`,
  `permissions`, `roles`, `role_permissions`, `role_bindings`,
  `scim_identities`, `audit_log`.
- Platform stages: add only the schema needed for their enabled platform
  capabilities.

If a later implementation needs to move a table between stages, the owning
implementation spec must be updated together with roadmap, traceability and test
plan.

## Base Entities

### `root_paths`

- `id`
- `name`
- `path`
- `source`
- `enabled`
- `schedule_enabled`
- `schedule_frequency`
- `created_at`
- `updated_at`

`source` accepts `config` or `api`. `scanning.global_scan` sync owns only
`source = config` rows: removed config roots are disabled, while API-created
roots are preserved.

### `projects`

- `id`
- `name`
- `path`
- `relative_path`
- `root_path_id`
- `terraform_marker`
- `status`
- `repository_id`
- `environment_id`
- `default_workspace_id`
- `detected_at`
- `last_seen_at`
- `created_at`
- `updated_at`

Allowed `status` values for Stage 04:

- `active` - the project was found by the latest scan or explicitly created/updated;
- `missing` - the project was previously found under a scanned `root_path`, but was not found in the latest completed scan;
- `disabled` - the project is administratively disabled and must not automatically participate in project scan workflows.

Invariants:

- `projects.root_path_id + projects.relative_path` unique;
- Stage 04 scanner creates/updates projects by `root_path_id + relative_path`;
- every locally discovered Terraform working directory remains a separate `projects` row, even when multiple projects belong to the same Git repository;
- project rows are created with the full schema from the beginning; fields whose information is not known at creation time remain nullable/default and are filled by later discovery/scanning/management stages;
- missing projects are not deleted by scanner;
- repeat scan must preserve known `repository_id`, `environment_id` and `default_workspace_id` unless an explicit update changes them.

### `project_links`

- `id`
- `source_project_id`
- `target_project_id`
- `link_type`
- `repository_id`
- `detected_by_job_id`
- `created_at`
- `updated_at`

Allowed `link_type` values:

- `same_repository`

Invariants:

- project links express relationships between separate local project records; they do not merge projects;
- `source_project_id` and `target_project_id` must reference different project rows;
- for `same_repository`, `repository_id` should reference the repository shared by both projects when known;
- duplicate links for the same unordered project pair and `link_type` must not be created.

### `project_scan_settings`

- `id`
- `project_id`
- `scan_enabled`
- `schedule_enabled`
- `schedule_frequency`
- `run_after_clone`
- `run_after_pull`
- `scan_type`
- `created_at`
- `updated_at`

Project-level scan settings are configured per project and are not global default parameters in `config.json`.

### `project_security_scan_settings`

- `id`
- `project_id`
- `enabled`
- `enabled_modules`
- `schedule_enabled`
- `schedule_frequency`
- `validate_code`
- `created_at`
- `updated_at`

`enabled_modules` must reference only modules from the global catalog `scanning.security_scan.modules`.

### `repositories`

- `id`
- `name`
- `provider_instance_id`
- `provider`
- `provider_host`
- `full_path`
- `clone_url`
- `default_branch`
- `root_path_id`
- `target_directory`
- `local_path`
- `auth_type`
- `default_credential_id`
- `status`
- `discovery_source`
- `superseded_by_repository_id`
- `identity_confirmed_at`
- `auto_sync_enabled`
- `webhook_enabled`
- `poll_interval`
- `last_pull_at`
- `last_error`
- `created_at`
- `updated_at`

Invariants:

- `provider` accepts one of these values: `gitlab`, `github`, `bitbucket`, `azure_devops`, `generic`;
- `status` accepts `active`, `missing`, `superseded` or `disabled`;
- `discovery_source` accepts `filesystem`, `provider`, `clone` or `manual`;
- `provider_instance_id` references a configured provider host/profile when the repository was created through a managed provider integration;
- `provider_host` stores the normalized host provider instance, for example `gitlab.foodtech.team` or `github.com`;
- repository identity is defined by the tuple `provider + provider_host + full_path`;
- for Stage 04 filesystem-only generic local cards
  (`provider = generic`, `provider_host = local`), identity is scoped by
  `root_path_id + full_path` so different scan roots with the same relative
  repository path do not collapse into one repository card;
- `full_path` is unique only within `provider + provider_host`;
- `full_path` stores the namespace/project path inside the provider instance without protocol, host, leading slash or trailing `.git`;
- `root_path_id` is required for cloned repositories;
- `target_directory` stores the user-selected directory inside `root_path`;
- `local_path` is computed inside the selected `root_path` and must not point outside that path;
- `clone_url` is nullable and non-unique; it is a safe normalized transport endpoint and is not an identity key;
- `clone_url` is not used for lookup/upsert/deduplication and must not contain credentials, tokens, passwords or userinfo;
- different `clone_url` values that normalize to one `provider + provider_host + full_path` must reference one repository card;
- Stage 04 global filesystem scan does not create repository cards directly;
- Stage 04 `project_discovery` job may create/update conservative repository card with `provider = generic`, `provider_host = local`, `root_path_id = <containing root path>`, `full_path = <root_path-relative normalized repository path>`, `clone_url = null`, `status = active`, `discovery_source = filesystem` and `identity_confirmed_at = null`;
- Stage 05 repository manager may enrich generic local cards after provider-aware identity resolution, but separate project records must not be merged;
- if provider-aware identity is resolved and no managed repository card exists, the generic repository card is enriched in place and keeps the same `repositories.id`;
- if provider-aware identity is resolved and a managed repository card already exists, project relations are moved to the managed card and the generic card becomes `status = superseded`;
- `superseded_by_repository_id` may be set only when `status = superseded`;
- `superseded` repositories must not participate in clone, pull, sync, webhook or polling operations;
- `identity_confirmed_at` is set when provider-aware identity has been validated by repository manager or explicit provider workflow;
- `auth_type` is the legacy/default transport hint for the repository card and accepts `ssh`, `https` or `token`; actual auth material comes from `repository_credentials`;
- `default_credential_id` is nullable and points to the credential used by default for repository operations when the request did not pass an explicit `credential_id`;
- active `clone`, `pull`, `sync` for one `repository.id` must have at most one lock/job in `queued` or `running`.

### `repository_provider_instances`

- `id`
- `provider`
- `provider_host`
- `display_name`
- `deployment_type`
- `api_base_url`
- `web_base_url`
- `default_clone_protocol`
- `enabled`
- `created_at`
- `updated_at`

Invariants:

- Stage 05 MVP stores `display_name` as `name` and does not yet persist
  `deployment_type` or `default_clone_protocol`; those fields remain the
  forward data-model contract for later provider-profile expansion.
- `provider` accepts `gitlab`, `github`, `bitbucket`, `azure_devops` or `generic`;
- `deployment_type` accepts `cloud`, `self_managed`, `enterprise_server`, `data_center` or `organization`;
- `provider_host` normalizes host or organization host identifier and distinguishes multi-domain/on-premise installations;
- `provider + provider_host` unique;
- provider instance stores host/profile metadata only, not secrets.
- Stage 05 MVP accepts `api_base_url` and `web_base_url` only as HTTPS URLs
  without userinfo; their host must normalize to the same value as
  `provider_host`.

### `repository_credentials`

- `id`
- `provider_instance_id`
- `name`
- `auth_type`
- `username`
- `usages`
- `scope_hint`
- `token_ref`
- `password_ref`
- `private_key_ref`
- `passphrase_ref`
- `webhook_secret_ref`
- `last_validated_at`
- `last_error`
- `created_at`
- `updated_at`

Invariants:

- Stage 05 MVP persists a single `secret_ref` selected by `auth_type` instead
  of separate `token_ref`, `password_ref`, `private_key_ref`,
  `passphrase_ref` and `webhook_secret_ref` columns; the separate columns are
  the forward data-model contract for richer credential material.
- `provider_instance_id` is required;
- `auth_type` accepts `ssh_key`, `https_token`, `https_basic`, `oauth_token`, `app_password`, `webhook_secret`;
- `usages` contains one or more values: `git_transport`, `provider_api`, `webhook`;
- secret fields store only `secretref://...` values and never resolved secrets;
- MVP accepts only `secretref://env/...` secret refs;
- API responses mask secret refs and never return resolved secrets;
- one provider instance may have multiple credentials with different usages, scope hints and permissions;
- repository operations must validate that selected `credential_id` belongs to the same provider instance, supports the required usage and has an `auth_type` compatible with the selected transport protocol.

### `ignore_rules`

Stage 02 creates this table for imported system-level `.t-helper.ignore` rules
from `thelper-ctl -reconfigure`. Stage 04 owns scanner/API behavior for
`root_path` and `project` scopes.

- `id`
- `scope_type`
- `scope_id`
- `pattern`
- `origin`
- `sort_order`
- `created_at`
- `updated_at`

Allowed `origin` values:

- `ui`
- `config_import`
- `system_default`

Allowed `scope_type` values:

- `system`
- `root_path`
- `project`

For MVP, the matcher may support exclude-only rules. Negative `!pattern` rules must be stored without data loss and applied after full `.gitignore` semantics are added.
`sort_order` preserves import/API upsert order for future full `.gitignore` semantics.

### `environments`

- `id`
- `name`
- `code`
- `description`
- `created_at`
- `updated_at`

### `workspaces`

- `id`
- `project_id`
- `environment_id`
- `name`
- `is_default`
- `created_at`
- `updated_at`

### `config_entries`

- `id`
- `key`
- `value`
- `value_type`
- `scope`
- `version`
- `updated_at`
- `updated_by`

Minimum configuration keys:

- `system_settings.app_name`
- `system_settings.version`
- `system_settings.mode`
- `database.database_type`
- `database.database_path`
- `external_databases.enabled`
- `external_databases.provider`
- `external_databases.engine_flavor`
- `external_databases.host`
- `external_databases.port`
- `external_databases.username`
- `external_databases.password`
- `external_databases.database_name`
- `scanning.global_scan`
- `scanning.security_scan.modules`
- `repositories.default_auth_type`
- `repositories.poll_interval_default`
- `repositories.auto_sync_default`
- `security.active_rule_set_id`
- `api.listen_address`
- `auth.local_enabled`
- `workers.enabled`
- `workers.concurrency`
- `modules.enabled`
- `logging.level`
- `logging.format`
- `logging.log_path`

`external_databases.provider` accepts values `postgresql`, `mysql` or `mssql`.
`external_databases.engine_flavor` is optional and accepts values `standard` or `aurora`; it does not change the storage dialect, but is used for diagnostics, validation and operational guidance.

### `storage_profiles`

- `id`
- `slot`
- `provider`
- `engine_flavor`
- `status`
- `config_payload`
- `database_fingerprint`
- `last_migrated_from_profile_id`
- `created_at`
- `updated_at`

Allowed `slot` values:

- `current`
- `migration`
- `historical`

Allowed `status` values:

- `active`
- `migration_target`
- `migration_in_progress`
- `migration_succeeded`
- `migration_failed`
- `superseded`

Invariants:

- there can be only one profile with `slot = current` and `status = active`;
- `migration` profile may be updated through `thelper-ctl -reconfigure` or `thelper-ctl -migrate-db`;
- active runtime uses only the `current` profile;
- active DB switching is performed only after a successful `thelper-ctl -migrate-db`;
- the old DB configuration is not deleted after switching and remains historical/rollback metadata;
- `config_payload` stores only normalized storage settings and secret refs, not resolved secrets;
- `database_fingerprint` must not reveal DSN, path, credentials or userinfo.
  Stage 01 derives the health fingerprint from safe storage locator components
  and exposes only the opaque fingerprint through `GET /api/health`.

### `storage_provider_settings`

- `id`
- `storage_profile_id`
- `provider`
- `workers_concurrency`
- `worker_process_limit`
- `busy_timeout`
- `lease_duration`
- `heartbeat_interval`
- `sqlite_journal_mode`
- `sqlite_foreign_keys`
- `created_at`
- `updated_at`

Invariants:

- settings scoped to one storage profile/provider do not change another provider's settings;
- for `sqlite`, MVP requires `worker_process_limit = 1`, `workers_concurrency = 1`, `sqlite_journal_mode = WAL`, `sqlite_foreign_keys = true`;
- for `postgresql`, worker limits are installation-specific and may be higher than `sqlite`;
- provider-specific settings are applied by the selected storage adapter at runtime start.

### `module_states`

- `id`
- `module_name`
- `state`
- `pid`
- `host`
- `details`
- `updated_at`

Allowed `state` values:

- `starting`
- `running`
- `reloading`
- `restarting`
- `degraded`
- `stopped`
- `unavailable`
- `failed`

`unavailable` means the module is known to the registry, but is unavailable in the current build, launch profile or implementation stage. `restart`/`reload` for such a module must return a controlled error.

Initial module registry seed:

- `core`
- `worker-runtime`
- `config-manager`
- `module-runtime`
- `status-monitor`
- `global-scanner`
- `repository-manager`
- `project-scanner`
- `security-validator`
- `auth`
- `web`

### `jobs`

- `id`
- `job_type`
- `status`
- `actor`
- `correlation_id`
- `idempotency_key`
- `parent_job_id`
- `job_group_id`
- `lock_key`
- `attempt_count`
- `max_attempts`
- `leased_by`
- `lease_expires_at`
- `heartbeat_at`
- `run_after`
- `priority`
- `payload`
- `result_payload`
- `created_at`
- `started_at`
- `finished_at`
- `error_message`
- `updated_at`

Allowed `job_type` values:

- `global_scan`
- `project_discovery`
- `project_scan`
- `security_validation_scan`
- `repo_clone`
- `repo_pull`
- `repo_sync`
- `config_reload`
- `module_restart`
- `scim_sync`

Allowed `status` values:

- `queued`
- `running`
- `succeeded`
- `failed`
- `cancelled`

Invariants:

- `idempotency_key` is unique for repeatable write requests within a reasonable TTL;
- `parent_job_id` is nullable and references the parent orchestration job in `jobs.id`;
- `job_group_id` groups jobs of one workflow, for example `project_scan:<project_scan_id>`;
- child job must use the same `job_group_id` as the parent job;
- `lock_key` is populated for operations that must be serialized;
- `leased_by` is populated only after an atomic claim by a specific `thelper-worker`;
- `lease_expires_at` defines the worker process ownership period for the job;
- `heartbeat_at` is updated by the worker process during long execution;
- an expired lease allows returning the job to `queued` or finishing it as `failed` if `max_attempts` is exhausted;
- `run_after` is used for delayed start and retry/backoff;
- `attempt_count` must not exceed `max_attempts`;
- the job lease owns a specific job, while `job_locks` serialize business resources;
- `payload` and `result_payload` have a versioned JSON schema described in [`payload-schemas.md`](payload-schemas.md);
- an active job has status `queued` or `running`.

### `job_locks`

- `id`
- `lock_key`
- `job_id`
- `owner`
- `status`
- `created_at`
- `expires_at`
- `released_at`

Allowed `status` values:

- `held`
- `released`
- `expired`

Invariants:

- only one lock with status `held` for one `lock_key`;
- for repository operations, the final `lock_key` is built as `repository:<repository_id>`, to serialize `clone`, `pull` and `sync` with each other;
- clone additionally uses pre-create lock keys `repository-identity:<provider>:<provider_host>:<full_path>` and `repository-path:<root_path_id>:<normalized_target_path>` before a stable `repository_id` exists;
- released/expired repository-operation reservations are removed by an explicit
  cleanup storage primitive and may also be pruned opportunistically before new
  reservations are created;
- expired locks must not block new operations, but must be retained for audit/debug.

### `project_scans`

- `id`
- `job_id`
- `project_id`
- `rule_set_id`
- `scan_type`
- `status`
- `created_at`
- `started_at`
- `finished_at`
- `result_payload`
- `error_message`
- `updated_at`

Allowed `scan_type` values:

- `terraform_static`
- `terraform_validate`
- `terraform_security`
- `terraform_full`
- `security_validation`

Allowed `status` values:

- `queued`
- `running`
- `succeeded`
- `failed`
- `partial`
- `cancelled`

`project_scans.job_id` points to the parent `jobs.job_type = project_scan`.
Aggregate `project_scans.status` and `project_scans.result_payload` are updated by `status-monitor`, not by individual worker handlers.

### `job_events`

- `id`
- `job_id`
- `job_group_id`
- `event_type`
- `status`
- `worker_id`
- `metric_name`
- `metric_value`
- `payload`
- `created_at`

Allowed base `event_type` values:

- `queued`
- `claimed`
- `started`
- `heartbeat`
- `progress`
- `child_created`
- `succeeded`
- `failed`
- `cancelled`
- `lease_expired`
- `retry_scheduled`

Invariants:

- `job_events` describe job execution facts and are not aggregate state;
- payload must not contain secrets, tokens, private keys or raw Terraform source;
- workers write `job_events`, and `status-monitor` builds aggregate read models.

### `workflow_statuses`

- `id`
- `workflow_type`
- `workflow_id`
- `job_group_id`
- `aggregate_status`
- `progress_current`
- `progress_total`
- `summary_payload`
- `updated_at`

Allowed base `workflow_type` values:

- `project_scan`
- `project_discovery`
- `global_scan`
- `repository_operation`
- `config_operation`
- `module_operation`
- `scim_sync`

Allowed `aggregate_status` values:

- `queued`
- `running`
- `succeeded`
- `failed`
- `partial`
- `cancelled`

Invariants:

- `workflow_statuses.workflow_type + workflow_id` unique;
- `workflow_statuses.job_group_id` unique;
- `workflow_statuses` are a read model owned by `status-monitor`;
- UI and internal services read workflow status from the aggregate read model instead of assembling it themselves from child jobs.

### `audit_log`

- `id`
- `actor`
- `action`
- `entity_type`
- `entity_id`
- `payload`
- `created_at`

## Security Entities

### `security_rule_sets`

- `id`
- `name`
- `version`
- `source_type`
- `checksum`
- `active`
- `created_at`
- `updated_at`

Allowed `source_type` values:

- `bundled`
- `local_upload`
- `local_path`

### `tool_profiles`

- `id`
- `tool`
- `profile_id`
- `profile_version`
- `schema_version`
- `source_type`
- `source_path`
- `checksum`
- `certified_versions`
- `compatible_versions`
- `active`
- `created_at`
- `updated_at`

Allowed `tool` values for Stage 06A/06B MVP:

- `terraform`
- `tflint`
- `trivy`

`checkov`, `gitleaks`, `opa` and `conftest` are extension tool/profile targets outside mandatory Stage 06A/06B MVP acceptance.

Allowed `source_type` values:

- `bundled`
- `local_upload`
- `local_path`
- `generated_candidate`

Invariants:

- `tool + profile_id + profile_version` unique;
- the active profile is selected by `tool`, configured version policy and discovered tool version;
- bundled certified profiles are required for `terraform validate`, `TFLint` and `Trivy`;
- profile files follow ADR 0018 and must pass validation before activation;
- `generated_candidate` profiles created by `tool-profile-analyzer` are inactive by default and must never be selected by runtime until explicitly validated and activated;
- profile content must not contain raw secrets, shell fragments, arbitrary scripts or commands outside explicit version discovery and scan command templates.

### `tool_profile_validation_results`

- `id`
- `tool_profile_id`
- `tool`
- `tool_version`
- `fixture_set`
- `validation_status`
- `diagnostics`
- `created_at`

Allowed `validation_status` values:

- `passed`
- `failed`
- `warning`

Invariants:

- validation results store fixture diagnostics and profile validation metadata, not raw Terraform source or secrets;
- activation of local/generated profiles requires at least one successful validation result for the target tool version or approved compatible version range.

### `security_findings`

- `id`
- `project_id`
- `repository_id`
- `workspace_id`
- `job_id`
- `rule_set_id`
- `check_type`
- `rule_id`
- `severity`
- `status`
- `file_path`
- `resource_ref`
- `title`
- `description`
- `remediation`
- `fingerprint`
- `fingerprint_schema_version`
- `fingerprint_components`
- `first_seen_at`
- `last_seen_at`
- `detected_at`
- `updated_at`

Allowed `severity` values:

- `info`
- `low`
- `medium`
- `high`
- `critical`

Allowed `status` values:

- `open`
- `accepted`
- `false_positive`
- `fixed`
- `suppressed`

Invariants:

- `fingerprint` has format `fp:v1:<sha256>` according to ADR 0017;
- `fingerprint_schema_version` accepts `security_finding.fingerprint.v1` for MVP;
- `fingerprint_components` stores canonical JSON components without secrets or raw Terraform source;
- `resource_ref` or stable `finding_key` inside `fingerprint_components` is required for a persisted finding;
- `first_seen_at` records the first fingerprint detection;
- `last_seen_at` is updated on every scan where the finding is present;
- `detected_at` is stored as a backward-compatible alias/initial detection timestamp and must match `first_seen_at` for new rows.

### `project_scan_findings`

- `project_scan_id`
- `finding_id`
- `detected_at`

Invariants:

- the `project_scan_id + finding_id` pair is unique;
- the table records which scan observed a deduplicated finding and backs the scoped findings API;
- deleting a project scan or finding cascades only to the association.

## Auth and RBAC Entities

### `users`

- `id`
- `username`
- `email`
- `display_name`
- `auth_source`
- `external_id`
- `active`
- `created_at`
- `updated_at`

`users` is the shared identity entity for local and external auth sources. Local password hashes are not stored in `users`.

### `local_user_credentials`

- `id`
- `user_id`
- `password_hash`
- `password_hash_algorithm`
- `password_updated_at`
- `password_must_change`
- `failed_attempt_count`
- `locked_until`
- `created_at`
- `updated_at`

Invariants:

- `user_id` is required and unique;
- `password_hash` stores the Argon2id PHC string;
- `password_hash_algorithm` accepts `argon2id` in MVP;
- raw password is never stored;
- successful login resets `failed_attempt_count`;
- 5 consecutive failed attempts set `locked_until` to 15 minutes from the lock moment;
- if Argon2id PHC parameters are weaker than current defaults, the hash is updated after successful login.

### `password_reset_tokens`

- `id`
- `user_id`
- `token_hash`
- `expires_at`
- `used_at`
- `created_at`

Invariants:

- reset token is stored only as a hash;
- raw reset token is never stored;
- token is one-time-use and invalid after `used_at` or `expires_at`;
- completing the reset flow sets `local_user_credentials.password_must_change = true`, unless a separate administrative policy defines different behavior.

### `user_sessions`

- `id`
- `user_id`
- `token_hash`
- `auth_source`
- `created_at`
- `expires_at`
- `revoked_at`
- `last_seen_at`

Invariants:

- `user_id` is required and references `users.id`;
- raw session token is never stored;
- `token_hash` stores only the hash of the opaque session token;
- session is invalid after `revoked_at` or `expires_at`;
- API responses return only opaque `session_id` and safe user metadata, but not the bearer token hash.

### `auth_bootstrap_credentials`

- `id`
- `user_id`
- `username`
- `password_hash`
- `shown_at`
- `expires_at`
- `used_at`
- `created_at`

Invariants:

- bootstrap user is created automatically only on the first start of `thelper` with empty auth state;
- username and initial password are generated as random Latin letters and digits, 16 characters each;
- password is stored only as an Argon2id PHC hash through `local_user_credentials`; raw password is kept only in memory until single display;
- credentials are shown once in the first launched UI and in active runtime stdout;
- stdout/UI warning must explicitly state that bootstrap credentials are valid for 24 hours;
- if the bootstrap user is not used within 24 hours, it is deleted together with credentials;
- after bootstrap user expiration or deletion, a new bootstrap user is not created automatically;
- if auth was not configured and bootstrap credentials expired, recovery requires full data deletion and a restart as a new empty installation.

### `groups`

- `id`
- `name`
- `external_id`
- `description`
- `created_at`
- `updated_at`

### `group_members`

- `id`
- `group_id`
- `user_id`
- `created_at`

### `permissions`

- `id`
- `code`
- `scope_type`
- `description`
- `created_at`

### `roles`

- `id`
- `name`
- `scope_type`
- `description`
- `is_system`
- `created_at`
- `updated_at`

### `role_permissions`

- `id`
- `role_id`
- `permission_id`
- `created_at`

### `role_bindings`

- `id`
- `subject_type`
- `subject_id`
- `role_id`
- `scope_type`
- `scope_id`
- `created_at`
- `updated_at`

### `scim_identities`

- `id`
- `user_id`
- `group_id`
- `provider`
- `external_id`
- `raw_payload`
- `last_sync_at`
- `created_at`
- `updated_at`

## Entity Relationships

- `root_path` contains many discovered `projects`
- `project` may be linked to one `repository`
- `project_link` relates two separate `projects`, optionally through one shared `repository`
- `repository` belongs to one optional `repository_provider_instance`
- `repository` can reference one default `repository_credential`
- `repository_provider_instance` has many `repository_credentials`
- `project` may have several `workspaces`
- `workspace` belongs to the `project` + `environment` pair
- `project_scan` belongs to one `project`
- `security_finding` may reference `project`, `repository`, `workspace`, `job`, `security_rule_set`
- `tool_profile_validation_result` belongs to one `tool_profile`
- `job_lock` belongs to one active or completed `job`
- `local_user_credentials` belongs to one local-auth `user`
- `password_reset_tokens` belongs to one `user`
- `user_sessions` belongs to one `user`
- `auth_bootstrap_credentials` belongs to one bootstrap `user`
- `role_binding` binds `user` or `group` to a role in a specific scope

## Nullable, FK and delete behavior

Minimum rules for SQL adapters:

- `projects.root_path_id` is required and uses `ON DELETE RESTRICT`;
- `project_links.source_project_id` and `project_links.target_project_id` are required and use `ON DELETE CASCADE`;
- `project_links.repository_id` is nullable and uses `ON DELETE SET NULL`;
- `project_scan_settings.project_id` is required and uses `ON DELETE CASCADE`;
- `project_security_scan_settings.project_id` is required and uses `ON DELETE CASCADE`;
- `projects.repository_id`, `projects.environment_id`, `projects.default_workspace_id` are nullable and use `ON DELETE SET NULL`;
- `repositories.provider_instance_id` is nullable and uses `ON DELETE SET NULL`;
- `repositories.default_credential_id` is nullable and uses `ON DELETE SET NULL`;
- `repositories.superseded_by_repository_id` is nullable and uses `ON DELETE SET NULL`;
- `repository_credentials.provider_instance_id` is required and uses `ON DELETE CASCADE`;
- `workspaces.project_id` and `workspaces.environment_id` are required and use `ON DELETE CASCADE` for `project`, `ON DELETE RESTRICT` for `environment`;
- `project_scans.job_id` and `project_scans.project_id` are required; deletion of related `jobs` and `projects` must use `ON DELETE RESTRICT`;
- `job_locks.job_id` is required; deletion of related `jobs` must use `ON DELETE RESTRICT`;
- `security_findings.project_id`, `security_findings.repository_id`, `security_findings.workspace_id`, `security_findings.job_id`, `security_findings.rule_set_id` are nullable, but a finding must reference at least one of `project_id`, `repository_id`, `workspace_id` or `job_id`;
- `tool_profile_validation_results.tool_profile_id` is required and uses `ON DELETE CASCADE`;
- `local_user_credentials.user_id` is required and uses `ON DELETE CASCADE`;
- `password_reset_tokens.user_id` is required and uses `ON DELETE CASCADE`;
- `user_sessions.user_id` is required and uses `ON DELETE CASCADE`;
- `role_bindings.scope_id = NULL` only for `system` scope; for object scopes `scope_id` is required;
- `scim_identities` must reference exactly one subject: `user_id` or `group_id`;
- audit records are not cascade-deleted together with business entities.

## Indexes and Uniqueness

Minimum set for SQL adapters:

- `root_paths.path` unique;
- `projects.root_path_id + projects.relative_path` unique;
- `project_links.source_project_id + project_links.target_project_id + project_links.link_type` unique after canonical ordering of project ids;
- `project_links.repository_id` indexed;
- `project_scan_settings.project_id` unique;
- `project_security_scan_settings.project_id` unique;
- provider-aware repositories use unique
  `repositories.provider + repositories.provider_host + repositories.full_path`;
- Stage 04 generic local repositories use unique
  `repositories.provider + repositories.provider_host + repositories.root_path_id + repositories.full_path`;
- `repositories.provider_host + repositories.full_path` indexed;
- `repositories.provider_instance_id + repositories.full_path` indexed;
- `repositories.local_path` indexed;
- `repositories.status` indexed;
- `repositories.discovery_source` indexed;
- `repositories.superseded_by_repository_id` indexed;
- `repository_provider_instances.provider + repository_provider_instances.provider_host` unique;
- `repository_credentials.provider_instance_id + repository_credentials.name` unique;
- `repository_credentials.provider_instance_id + repository_credentials.auth_type` indexed;
- `ignore_rules.scope_type + ignore_rules.scope_id + ignore_rules.pattern` unique;
- `environments.code` unique;
- `workspaces.project_id + workspaces.name` unique;
- `config_entries.key + config_entries.scope` unique;
- `jobs.idempotency_key` unique where not null;
- `jobs.parent_job_id` indexed;
- `jobs.job_group_id + jobs.status` indexed;
- `jobs.lock_key + jobs.status` indexed for active job lookup;
- `jobs.status + run_after + priority + created_at` indexed for worker claim;
- `jobs.lease_expires_at` indexed for expired lease recovery;
- `jobs.leased_by + status` indexed for worker diagnostics;
- `job_locks.lock_key` unique where `status = held`;
- `project_scans.job_id` unique;
- `project_scans.project_id + project_scans.status` indexed;
- `job_events.job_group_id + job_events.created_at` indexed;
- `job_events.job_id + job_events.created_at` indexed;
- `workflow_statuses.workflow_type + workflow_statuses.workflow_id` unique;
- `workflow_statuses.job_group_id` unique;
- `security_findings.fingerprint` unique;
- `security_findings.rule_set_id + security_findings.status` indexed;
- `security_findings.project_id + security_findings.status` indexed;
- `security_findings.repository_id + security_findings.status` indexed;
- `security_findings.job_id` indexed;
- `security_findings.severity + security_findings.status` indexed;
- `tool_profiles.tool + tool_profiles.profile_id + tool_profiles.profile_version` unique;
- `tool_profiles.tool + tool_profiles.active` indexed;
- `tool_profile_validation_results.tool_profile_id + tool_profile_validation_results.tool_version` indexed;
- `tool_profile_validation_results.validation_status` indexed;
- `users.username` unique;
- `users.email` unique where not null;
- `local_user_credentials.user_id` unique;
- `password_reset_tokens.user_id + expires_at` indexed;
- `password_reset_tokens.expires_at` indexed;
- `user_sessions.user_id + expires_at` indexed;
- `user_sessions.token_hash` unique;
- `user_sessions.expires_at` indexed;
- `groups.name` unique;
- `permissions.code` unique;
- `roles.name + roles.scope_type` unique;
- `role_bindings.subject_type + role_bindings.subject_id + role_bindings.role_id + role_bindings.scope_type + role_bindings.scope_id` unique.

## Cross-Storage Rules

- SQL/SQL-like backends implement invariants through migrations, constraints and indexes where supported.
- MVP storage adapters: `PostgreSQL` and `SQLite`.
- Platform storage adapters: `MySQL` and `MSSQL`.
- Aurora PostgreSQL is supported through the `postgresql` adapter; Aurora MySQL is supported through the `mysql` adapter.
- Babelfish for Aurora PostgreSQL is not considered an `mssql` adapter target without a separate compatibility decision.
- All SQL adapters use synchronized logical migration versions with dialect-specific SQL.
- Adapters with limited SQL constraint support must duplicate critical checks at the application level.
- Times are stored in UTC.
- Identifiers must be opaque to API consumers.
- `payload`, `details` and `result_payload` must have a versioned JSON schema described in [`payload-schemas.md`](payload-schemas.md).

## Design Notes

- the model remains common for SQL and non-SQL backends;
- SQL adapters must support migrations;
- critical invariants must be validated both at the storage level and at the application level;
- findings and project scans are best designed as append-oriented entities with explicit status and timestamps.
