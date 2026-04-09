# Модель данных

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
- `repo_id`
- `environment_id`
- `default_workspace_id`
- `detected_at`
- `last_seen_at`
- `created_at`
- `updated_at`

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
- `provider`
- `full_path`
- `clone_url`
- `default_branch`
- `root_path_id`
- `target_directory`
- `local_path`
- `auth_type`
- `auto_sync_enabled`
- `webhook_enabled`
- `poll_interval`
- `last_pull_at`
- `last_error`
- `created_at`
- `updated_at`

Инварианты:

- `provider` принимает одно из значений: `gitlab`, `github`, `bitbucket`, `generic`;
- `full_path` уникален в пределах установки;
- `root_path_id` обязателен для клонированных репозиториев;
- `target_directory` хранит выбранную пользователем директорию внутри `root_path`;
- `local_path` вычисляется внутри выбранного `root_path` и не должен указывать за пределы этого path;
- `clone_url` уникален, если один remote не должен мапиться на несколько карточек репозитория;
- `auth_type` принимает одно из значений: `ssh`, `https`, `token`;
- активные `clone`, `pull`, `sync` по одному `repository.id` должны иметь не более одного lock/job в состоянии `queued` или `running`.

### `ignore_rules`

- `id`
- `scope_type`
- `scope_id`
- `pattern`
- `origin`
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
- `external_databases.host`
- `external_databases.port`
- `external_databases.username`
- `external_databases.password`
- `external_databases.database_name`
- `scanning.global_scann`
- `scanning.security_scan.modules`
- `repositories.default_auth_type`
- `repositories.poll_interval_default`
- `repositories.auto_sync_default`
- `security.active_rule_set_id`
- `api.listen_address`
- `auth.local_enabled`
- `modules.enabled`
- `logging.level`
- `logging.format`
- `logging.log_path`

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
- `failed`

### `jobs`

- `id`
- `job_type`
- `status`
- `actor`
- `correlation_id`
- `idempotency_key`
- `lock_key`
- `payload`
- `result_payload`
- `created_at`
- `started_at`
- `finished_at`
- `error_message`
- `updated_at`

Допустимые `job_type`:

- `global_scan`
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
- `lock_key` заполняется для операций, которые должны сериализоваться;
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
- для repository operations `lock_key` строится как `repository:<repository_id>`, чтобы сериализовать `clone`, `pull` и `sync` между собой;
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

Допустимые `status` совпадают с `jobs.status`.

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
- `project` может иметь несколько `workspaces`
- `workspace` принадлежит паре `project` + `environment`
- `project_scan` относится к одному `project`
- `security_finding` может ссылаться на `project`, `repository`, `workspace`, `job`, `security_rule_set`
- `job_lock` относится к одному активному или завершённому `job`
- `role_binding` связывает `user` или `group` с ролью в конкретном scope

## Nullable, FK и delete behavior

Минимальные правила для SQL adapters:

- `projects.root_path_id` обязателен и использует `ON DELETE RESTRICT`;
- `project_scan_settings.project_id` обязателен и использует `ON DELETE CASCADE`;
- `project_security_scan_settings.project_id` обязателен и использует `ON DELETE CASCADE`;
- `projects.repo_id`, `projects.environment_id`, `projects.default_workspace_id` nullable и используют `ON DELETE SET NULL`;
- `workspaces.project_id` и `workspaces.environment_id` обязательны и используют `ON DELETE CASCADE` для `project`, `ON DELETE RESTRICT` для `environment`;
- `project_scans.job_id` и `project_scans.project_id` обязательны; удаление связанных `jobs` и `projects` должно использовать `ON DELETE RESTRICT`;
- `job_locks.job_id` обязателен; удаление связанных `jobs` должно использовать `ON DELETE RESTRICT`;
- `security_findings.project_id`, `security_findings.repository_id`, `security_findings.workspace_id`, `security_findings.job_id`, `security_findings.rule_set_id` nullable, но finding должен ссылаться хотя бы на один из `project_id`, `repository_id`, `workspace_id` или `job_id`;
- `role_bindings.scope_id = NULL` только для `system` scope; для объектных scopes `scope_id` обязателен;
- `scim_identities` должен ссылаться ровно на один субъект: `user_id` или `group_id`;
- audit records не удаляются каскадно вместе с бизнес-сущностями.

## Индексы и уникальность

Минимальный набор для SQL adapters:

- `root_paths.path` unique;
- `projects.root_path_id + projects.relative_path` unique;
- `project_scan_settings.project_id` unique;
- `project_security_scan_settings.project_id` unique;
- `repositories.full_path` unique;
- `ignore_rules.scope_type + ignore_rules.scope_id + ignore_rules.pattern` unique;
- `environments.code` unique;
- `workspaces.project_id + workspaces.name` unique;
- `config_entries.key + config_entries.scope` unique;
- `jobs.idempotency_key` unique where not null;
- `jobs.lock_key + jobs.status` indexed for active job lookup;
- `job_locks.lock_key` unique where `status = held`;
- `project_scans.job_id` unique;
- `project_scans.project_id + project_scans.status` indexed;
- `security_findings.fingerprint + security_findings.rule_set_id` indexed;
- `security_findings.project_id + security_findings.status` indexed;
- `security_findings.repository_id + security_findings.status` indexed;
- `security_findings.job_id` indexed;
- `security_findings.severity + security_findings.status` indexed;
- `users.username` unique;
- `users.email` unique where not null;
- `groups.name` unique;
- `permissions.code` unique;
- `roles.name + roles.scope_type` unique;
- `role_bindings.subject_type + role_bindings.subject_id + role_bindings.role_id + role_bindings.scope_type + role_bindings.scope_id` unique.

## Cross-storage правила

- SQL backends реализуют инварианты через миграции, constraints и индексы там, где это поддерживается.
- `Badger` и `MongoDB` adapters должны дублировать критичные проверки на уровне приложения.
- Время хранится в UTC.
- Идентификаторы должны быть opaque для API consumers.
- `payload`, `details` и `result_payload` должны иметь версионируемую JSON-схему, описанную в [`payload-schemas.md`](payload-schemas.md).

## Дизайн-заметки

- модель остаётся общей для SQL и non-SQL backends;
- SQL-адаптеры должны поддерживать миграции;
- критичные инварианты должны валидироваться и на уровне storage, и на уровне приложения;
- findings и project scans лучше проектировать как append-oriented сущности с явным статусом и timestamps.
