# Модель данных

Этот документ описывает целевую persistent data model. Он не означает, что все
таблицы должны появиться в первой миграции.

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

## Базовые сущности

### `root_paths`

- `id`
- `name`
- `path`
- `enabled`
- `schedule_enabled`
- `schedule_frequency`
- `created_at`
- `updated_at`

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

Допустимые `status` для Stage 04:

- `active` - проект обнаружен последним scan или создан/обновлён явно;
- `missing` - проект ранее обнаруживался под scanned `root_path`, но не найден в последнем completed scan;
- `disabled` - проект выключен административно и не должен автоматически участвовать в project scan workflows.

Инварианты:

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

Допустимые `link_type`:

- `same_repository`

Инварианты:

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

Настройки project-level scan задаются относительно отдельного проекта и не являются глобальными default-параметрами `config.json`.

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

`enabled_modules` должен ссылаться только на модули из глобального каталога `scanning.security_scan.modules`.

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

Инварианты:

- `provider` принимает одно из значений: `gitlab`, `github`, `bitbucket`, `azure_devops`, `generic`;
- `status` принимает `active`, `missing`, `superseded` или `disabled`;
- `discovery_source` принимает `filesystem`, `provider`, `clone` или `manual`;
- `provider_instance_id` ссылается на configured provider host/profile, если repository был создан через managed provider integration;
- `provider_host` хранит нормализованный host provider instance, например `gitlab.foodtech.team` или `github.com`;
- identity репозитория задаётся tuple `provider + provider_host + full_path`;
- for Stage 04 filesystem-only generic local cards
  (`provider = generic`, `provider_host = local`), identity is scoped by
  `root_path_id + full_path` so different scan roots with the same relative
  repository path do not collapse into one repository card;
- `full_path` уникален только в пределах `provider + provider_host`;
- `full_path` хранит namespace/project path внутри provider instance без protocol, host, leading slash и trailing `.git`;
- `root_path_id` обязателен для клонированных репозиториев;
- `target_directory` хранит выбранную пользователем директорию внутри `root_path`;
- `local_path` вычисляется внутри выбранного `root_path` и не должен указывать за пределы этого path;
- `clone_url` nullable, не unique, является safe normalized transport endpoint и не является identity key;
- `clone_url` не используется для lookup/upsert/deduplication и не должен содержать credentials, tokens, passwords или userinfo;
- разные `clone_url`, нормализуемые в один `provider + provider_host + full_path`, должны ссылаться на одну repository card;
- Stage 04 global filesystem scan does not create repository cards directly;
- Stage 04 `project_discovery` job may create/update conservative repository card with `provider = generic`, `provider_host = local`, `root_path_id = <containing root path>`, `full_path = <root_path-relative normalized repository path>`, `clone_url = null`, `status = active`, `discovery_source = filesystem` and `identity_confirmed_at = null`;
- Stage 05 repository manager may enrich generic local cards after provider-aware identity resolution, but separate project records must not be merged;
- if provider-aware identity is resolved and no managed repository card exists, the generic repository card is enriched in place and keeps the same `repositories.id`;
- if provider-aware identity is resolved and a managed repository card already exists, project relations are moved to the managed card and the generic card becomes `status = superseded`;
- `superseded_by_repository_id` may be set only when `status = superseded`;
- `superseded` repositories must not participate in clone, pull, sync, webhook or polling operations;
- `identity_confirmed_at` is set when provider-aware identity has been validated by repository manager or explicit provider workflow;
- `auth_type` является legacy/default transport hint для карточки repository и принимает `ssh`, `https` или `token`; actual auth material берётся из `repository_credentials`;
- `default_credential_id` nullable и указывает credential, используемый по умолчанию для repository operations, если request не передал explicit `credential_id`;
- активные `clone`, `pull`, `sync` по одному `repository.id` должны иметь не более одного lock/job в состоянии `queued` или `running`.

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

Инварианты:

- `provider` принимает `gitlab`, `github`, `bitbucket`, `azure_devops` или `generic`;
- `deployment_type` принимает `cloud`, `self_managed`, `enterprise_server`, `data_center` или `organization`;
- `provider_host` normalizes host or organization host identifier and distinguishes multi-domain/on-premise installations;
- `provider + provider_host` unique;
- provider instance stores host/profile metadata only, not secrets.

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

Инварианты:

- `provider_instance_id` обязателен;
- `auth_type` принимает `ssh_key`, `https_token`, `https_basic`, `oauth_token`, `app_password`, `webhook_secret`;
- `usages` содержит один или несколько values: `git_transport`, `provider_api`, `webhook`;
- secret fields store only `secretref://...` values and never resolved secrets;
- MVP accepts only `secretref://env/...` secret refs;
- API responses mask secret refs and never return resolved secrets;
- one provider instance may have multiple credentials with different usages, scope hints and permissions;
- repository operations must validate that selected `credential_id` belongs to the same provider instance and supports the required usage.

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

Допустимые `origin`:

- `ui`
- `config_import`
- `system_default`

Допустимые `scope_type`:

- `system`
- `root_path`
- `project`

Для MVP matcher может поддерживать только exclude-only правила. Отрицательные правила `!pattern` должны храниться без потери данных и применяться после добавления full `.gitignore` semantics.
`sort_order` сохраняет порядок импорта/API upsert для будущей полной `.gitignore` семантики.

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

Минимальные конфигурационные ключи:

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

`external_databases.provider` принимает значения `postgresql`, `mysql` или `mssql`.
`external_databases.engine_flavor` optional и принимает значения `standard` или `aurora`; он не меняет storage dialect, но используется для diagnostics, validation и operational guidance.

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

Допустимые `slot`:

- `current`
- `migration`
- `historical`

Допустимые `status`:

- `active`
- `migration_target`
- `migration_in_progress`
- `migration_succeeded`
- `migration_failed`
- `superseded`

Инварианты:

- одновременно может быть только один profile со `slot = current` и `status = active`;
- `migration` profile может обновляться через `thelper-ctl -reconfigure` или `thelper-ctl -migrate-db`;
- active runtime использует только `current` profile;
- переключение active DB выполняется только после successful `thelper-ctl -migrate-db`;
- старая DB configuration не удаляется после переключения и остаётся historical/rollback metadata;
- `config_payload` хранит normalized storage settings and secret refs only, not resolved secrets;
- `database_fingerprint` не должен раскрывать DSN, path, credentials или userinfo.
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

Инварианты:

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

Допустимые `state`:

- `starting`
- `running`
- `reloading`
- `restarting`
- `degraded`
- `stopped`
- `unavailable`
- `failed`

`unavailable` означает, что модуль известен registry, но недоступен в текущей сборке, профиле запуска или stage реализации. `restart`/`reload` такого модуля должен возвращать controlled error.

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

Допустимые `job_type`:

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

Допустимые `status`:

- `queued`
- `running`
- `succeeded`
- `failed`
- `cancelled`

Инварианты:

- `idempotency_key` уникален для повторяемых write requests в пределах разумного TTL;
- `parent_job_id` nullable и ссылается на parent orchestration job в `jobs.id`;
- `job_group_id` объединяет jobs одного workflow, например `project_scan:<project_scan_id>`;
- child job должен использовать тот же `job_group_id`, что и parent job;
- `lock_key` заполняется для операций, которые должны сериализоваться;
- `leased_by` заполняется только после atomic claim конкретным `thelper-worker`;
- `lease_expires_at` определяет срок владения job worker-процессом;
- `heartbeat_at` обновляется worker-процессом во время долгого выполнения;
- истёкший lease позволяет вернуть job в `queued` или завершить его как `failed`, если исчерпаны `max_attempts`;
- `run_after` используется для отложенного запуска и retry/backoff;
- `attempt_count` не должен превышать `max_attempts`;
- job lease отвечает за владение конкретным job, а `job_locks` отвечают за сериализацию бизнес-ресурсов;
- `payload` и `result_payload` имеют версионируемую JSON-схему, описанную в [`payload-schemas.md`](payload-schemas.md);
- активный job имеет статус `queued` или `running`.

### `job_locks`

- `id`
- `lock_key`
- `job_id`
- `owner`
- `status`
- `created_at`
- `expires_at`
- `released_at`

Допустимые `status`:

- `held`
- `released`
- `expired`

Инварианты:

- одновременно может существовать только один lock со статусом `held` для одного `lock_key`;
- для repository operations final `lock_key` строится как `repository:<repository_id>`, чтобы сериализовать `clone`, `pull` и `sync` между собой;
- clone additionally uses pre-create lock keys `repository-identity:<provider>:<provider_host>:<full_path>` and `repository-path:<root_path_id>:<normalized_target_path>` before a stable `repository_id` exists;
- истёкшие locks не должны блокировать новые операции, но должны сохраняться для audit/debug.

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

Допустимые `scan_type`:

- `terraform_static`
- `terraform_validate`
- `terraform_security`
- `terraform_full`
- `security_validation`

Допустимые `status`:

- `queued`
- `running`
- `succeeded`
- `failed`
- `partial`
- `cancelled`

`project_scans.job_id` указывает на parent `jobs.job_type = project_scan`.
Aggregate `project_scans.status` и `project_scans.result_payload` обновляются `status-monitor`, а не отдельными worker handlers.

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

Допустимые базовые `event_type`:

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

Инварианты:

- `job_events` описывают факты выполнения jobs и не являются aggregate state;
- payload не должен содержать секреты, tokens, приватные ключи или raw Terraform source;
- workers пишут `job_events`, а `status-monitor` строит aggregate read models.

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

Допустимые базовые `workflow_type`:

- `project_scan`
- `project_discovery`
- `global_scan`
- `repository_operation`
- `config_operation`
- `module_operation`
- `scim_sync`

Допустимые `aggregate_status`:

- `queued`
- `running`
- `succeeded`
- `failed`
- `partial`
- `cancelled`

Инварианты:

- `workflow_statuses.workflow_type + workflow_id` unique;
- `workflow_statuses.job_group_id` unique;
- `workflow_statuses` являются read model, которым владеет `status-monitor`;
- UI и внутренние сервисы читают workflow status из aggregate read model, а не собирают его самостоятельно из child jobs.

### `audit_log`

- `id`
- `actor`
- `action`
- `entity_type`
- `entity_id`
- `payload`
- `created_at`

## Security-сущности

### `security_rule_sets`

- `id`
- `name`
- `version`
- `source_type`
- `checksum`
- `active`
- `created_at`
- `updated_at`

Допустимые `source_type`:

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

Допустимые `tool` для Stage 06A/06B MVP:

- `terraform`
- `tflint`
- `trivy`

`checkov`, `gitleaks`, `opa` и `conftest` are extension tool/profile targets outside mandatory Stage 06A/06B MVP acceptance.

Допустимые `source_type`:

- `bundled`
- `local_upload`
- `local_path`
- `generated_candidate`

Инварианты:

- `tool + profile_id + profile_version` unique;
- активный profile выбирается по `tool`, configured version policy и discovered tool version;
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

Допустимые `validation_status`:

- `passed`
- `failed`
- `warning`

Инварианты:

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

Допустимые `severity`:

- `info`
- `low`
- `medium`
- `high`
- `critical`

Допустимые `status`:

- `open`
- `accepted`
- `false_positive`
- `fixed`
- `suppressed`

Инварианты:

- `fingerprint` имеет формат `fp:v1:<sha256>` согласно ADR 0017;
- `fingerprint_schema_version` для MVP принимает `security_finding.fingerprint.v1`;
- `fingerprint_components` хранит canonical JSON components без секретов и raw Terraform source;
- `resource_ref` или stable `finding_key` внутри `fingerprint_components` обязателен для persisted finding;
- `first_seen_at` фиксирует первое обнаружение fingerprint;
- `last_seen_at` обновляется при каждом scan, где finding присутствует;
- `detected_at` сохраняется как backward-compatible alias/initial detection timestamp и должен совпадать с `first_seen_at` для новых rows.

## Auth и RBAC сущности

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

Инварианты:

- `user_id` обязателен и unique;
- `password_hash` хранит Argon2id PHC string;
- `password_hash_algorithm` в MVP принимает `argon2id`;
- raw password никогда не сохраняется;
- successful login сбрасывает `failed_attempt_count`;
- 5 consecutive failed attempts устанавливают `locked_until` на 15 минут от момента блокировки;
- если Argon2id PHC parameters слабее текущих defaults, hash обновляется после successful login.

### `password_reset_tokens`

- `id`
- `user_id`
- `token_hash`
- `expires_at`
- `used_at`
- `created_at`

Инварианты:

- reset token хранится только как hash;
- raw reset token никогда не сохраняется;
- token является one-time-use и недействителен после `used_at` или `expires_at`;
- завершение reset flow выставляет `local_user_credentials.password_must_change = true`, если отдельная administrative policy не задаёт другое поведение.

### `user_sessions`

- `id`
- `user_id`
- `token_hash`
- `auth_source`
- `created_at`
- `expires_at`
- `revoked_at`
- `last_seen_at`

Инварианты:

- `user_id` обязателен и ссылается на `users.id`;
- raw session token никогда не сохраняется;
- `token_hash` хранит только hash opaque session token;
- session недействительна после `revoked_at` или `expires_at`;
- API responses возвращают только opaque `session_id` и safe user metadata, но не bearer token hash.

### `auth_bootstrap_credentials`

- `id`
- `user_id`
- `username`
- `password_hash`
- `shown_at`
- `expires_at`
- `used_at`
- `created_at`

Инварианты:

- bootstrap user создаётся автоматически только при первом запуске `thelper` на пустой auth state;
- username и initial password генерируются как случайные латинские буквы и цифры длиной 16 символов каждый;
- password хранится только как Argon2id PHC hash через `local_user_credentials`, raw password сохраняется только в памяти до single display;
- credentials показываются один раз в первом запущенном UI и в stdout active runtime;
- stdout/UI warning должен явно указывать, что bootstrap credentials действуют 24 часа;
- если bootstrap user не был использован в течение 24 часов, он удаляется вместе с credentials;
- после истечения или удаления bootstrap user новый bootstrap user автоматически не создаётся;
- если auth не был настроен и bootstrap credentials истекли, восстановление требует полного удаления данных и повторного запуска для новой пустой установки.

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

## Связи между сущностями

- `root_path` содержит множество найденных `projects`
- `project` может быть связан с одним `repository`
- `project_link` relates two separate `projects`, optionally through one shared `repository`
- `repository` belongs to one optional `repository_provider_instance`
- `repository` can reference one default `repository_credential`
- `repository_provider_instance` has many `repository_credentials`
- `project` может иметь несколько `workspaces`
- `workspace` принадлежит паре `project` + `environment`
- `project_scan` относится к одному `project`
- `security_finding` может ссылаться на `project`, `repository`, `workspace`, `job`, `security_rule_set`
- `tool_profile_validation_result` belongs to one `tool_profile`
- `job_lock` относится к одному активному или завершённому `job`
- `local_user_credentials` belongs to one local-auth `user`
- `password_reset_tokens` belongs to one `user`
- `user_sessions` belongs to one `user`
- `auth_bootstrap_credentials` belongs to one bootstrap `user`
- `role_binding` связывает `user` или `group` с ролью в конкретном scope

## Nullable, FK и delete behavior

Минимальные правила для SQL adapters:

- `projects.root_path_id` обязателен и использует `ON DELETE RESTRICT`;
- `project_links.source_project_id` и `project_links.target_project_id` обязательны и используют `ON DELETE CASCADE`;
- `project_links.repository_id` nullable и использует `ON DELETE SET NULL`;
- `project_scan_settings.project_id` обязателен и использует `ON DELETE CASCADE`;
- `project_security_scan_settings.project_id` обязателен и использует `ON DELETE CASCADE`;
- `projects.repository_id`, `projects.environment_id`, `projects.default_workspace_id` nullable и используют `ON DELETE SET NULL`;
- `repositories.provider_instance_id` nullable и использует `ON DELETE SET NULL`;
- `repositories.default_credential_id` nullable и использует `ON DELETE SET NULL`;
- `repositories.superseded_by_repository_id` nullable и использует `ON DELETE SET NULL`;
- `repository_credentials.provider_instance_id` обязателен и использует `ON DELETE CASCADE`;
- `workspaces.project_id` и `workspaces.environment_id` обязательны и используют `ON DELETE CASCADE` для `project`, `ON DELETE RESTRICT` для `environment`;
- `project_scans.job_id` и `project_scans.project_id` обязательны; удаление связанных `jobs` и `projects` должно использовать `ON DELETE RESTRICT`;
- `job_locks.job_id` обязателен; удаление связанных `jobs` должно использовать `ON DELETE RESTRICT`;
- `security_findings.project_id`, `security_findings.repository_id`, `security_findings.workspace_id`, `security_findings.job_id`, `security_findings.rule_set_id` nullable, но finding должен ссылаться хотя бы на один из `project_id`, `repository_id`, `workspace_id` или `job_id`;
- `tool_profile_validation_results.tool_profile_id` обязателен и использует `ON DELETE CASCADE`;
- `local_user_credentials.user_id` обязателен и использует `ON DELETE CASCADE`;
- `password_reset_tokens.user_id` обязателен и использует `ON DELETE CASCADE`;
- `user_sessions.user_id` обязателен и использует `ON DELETE CASCADE`;
- `role_bindings.scope_id = NULL` только для `system` scope; для объектных scopes `scope_id` обязателен;
- `scim_identities` должен ссылаться ровно на один субъект: `user_id` или `group_id`;
- audit records не удаляются каскадно вместе с бизнес-сущностями.

## Индексы и уникальность

Минимальный набор для SQL adapters:

- `root_paths.path` unique;
- `projects.root_path_id + projects.relative_path` unique;
- `project_links.source_project_id + project_links.target_project_id + project_links.link_type` unique after canonical ordering of project ids;
- `project_links.repository_id` indexed;
- `project_scan_settings.project_id` unique;
- `project_security_scan_settings.project_id` unique;
- `repositories.provider + repositories.provider_host + repositories.full_path` unique;
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

## Cross-storage правила

- SQL/SQL-like backends реализуют инварианты через миграции, constraints и индексы там, где это поддерживается.
- MVP storage adapters: `PostgreSQL` и `SQLite`.
- Platform storage adapters: `MySQL` и `MSSQL`.
- Aurora PostgreSQL поддерживается через `postgresql` adapter; Aurora MySQL поддерживается через `mysql` adapter.
- Babelfish for Aurora PostgreSQL не считается `mssql` adapter target без отдельного compatibility decision.
- Все SQL adapters используют синхронизированные logical migration versions с dialect-specific SQL.
- Adapters с ограниченной поддержкой SQL constraints должны дублировать критичные проверки на уровне приложения.
- Время хранится в UTC.
- Идентификаторы должны быть opaque для API consumers.
- `payload`, `details` и `result_payload` должны иметь версионируемую JSON-схему, описанную в [`payload-schemas.md`](payload-schemas.md).

## Дизайн-заметки

- модель остаётся общей для SQL и non-SQL backends;
- SQL-адаптеры должны поддерживать миграции;
- критичные инварианты должны валидироваться и на уровне storage, и на уровне приложения;
- findings и project scans лучше проектировать как append-oriented сущности с явным статусом и timestamps.
