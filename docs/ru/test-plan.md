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
| `ACC-MVP-002` | Глобальное сканирование определяет Terraform-проекты, а фоновый project discovery определяет Git-связи проектов. | Fixture содержит `*.tf` и разрешённые Git markers `.git/` directory и `.git` file с `gitdir:`; проверить `projects`, queued `project_discovery` jobs, `repositories`/`project_links` после выполнения project discovery; проверить negative fixtures для похожих файлов, которые не должны создавать repository link/card. |
| `ACC-MVP-003` | После обнаружения проекта вложенные директории не сканируются как отдельные working directories. | Fixture содержит parent `main.tf` и nested `child/main.tf`; проверить отсутствие второго `project` под parent. |
| `ACC-MVP-004` | Ignore rules исключают файлы и директории; `!pattern` сохраняется. | Загрузить `.t-helper.ignore`; проверить `ignore_rules.pattern`, исключение directories и сохранение отрицательных patterns. |
| `ACC-MVP-005` | Проекты сохраняются отдельными записями и обновляются в БД; Git-связи проектов сохраняются без merge project records. | Запустить scan дважды; проверить отсутствие дублей по unique constraints и обновление `last_seen_at`; выполнить project discovery и проверить shared `repository_id`/`project_links` для проектов из одного Git repository без merge project rows. |
| `ACC-MVP-006` | Project-level scan определяет providers, required auth и quality через `terraform validate` и `TFLint`, а security/validation scan сохраняет findings через `Trivy` как обязательный локальный scanner. | Настроить `PUT /api/projects/{id}/scan-settings`, запустить `POST /api/project-scans`; проверить parent `jobs.job_type = project_scan`, child `jobs.job_type = security_validation_scan` с тем же `job_group_id`, `project_scans.result_payload`, `workflow_statuses` и `security_findings` для `Trivy`. |
| `ACC-MVP-007` | Runtime-конфигурация хранится в БД. | Импортировать config; проверить `config_entries` и `GET /api/config`. |
| `ACC-MVP-008` | `thelper-ctl -reconfigure` импортирует конфигурацию и ignore rules. | Запустить CLI command; проверить `config_entries`, `ignore_rules`, отсутствие service restart side effect. |
| `ACC-MVP-009` | `thelper-ctl -reload` применяет reloadable-конфигурацию. | Изменить `modules.enabled`, `logging.level` или `scanning.global_scan`; проверить sync reload result с `accepted_keys`, `applied_keys`, `restart_required_keys`, `failed_keys`, отсутствие Stage 03 `jobs` dependency and honest distinction between accepted and actually applied Stage 02 keys. |
| `ACC-MVP-010` | `thelper-ctl -restart <module>` работает для любого доступного отдельного модуля, а unavailable modules дают controlled error. | Перезапустить доступный module, например `config-manager` или `global-scanner`; проверить `module_states` transition и `module_restart.result.v1`; restart/reload unavailable roadmap modules должен вернуть controlled `module_unavailable`. |
| `ACC-MVP-011` | `GUI` и `Web UI` используют единый backend API и покрывают MVP read/operate сценарии. | Контрактный тест: оба frontend clients используют только documented backend API. |
| `ACC-MVP-012` | `GUI` работает только локально. | Проверить bind policy/config и отказ удалённого GUI access. |
| `ACC-MVP-013` | `PostgreSQL` и `SQLite` поддерживаются через storage abstraction. | Запустить один и тот же storage test suite против PostgreSQL и SQLite adapters. |
| `ACC-MVP-014` | `clone`, `pull`, `sync` сериализуются через `job_locks`, clone использует общий root path, а новый root path автоматически попадает в root paths. | Запустить конкурирующие repo operations; проверить один `held` lock и отсутствие конфликтующих active jobs; выполнить `POST /api/repos/clone` с `new_root_path` и проверить появление нового `root_path`; проверить clone в существующую и новую `target_directory`; проверить, что repository identity строится по `provider + provider_host + full_path`. |
| `ACC-MVP-022` | Clone workflow поддерживает `generic` Git и один managed provider из `gitlab` или `github`, выбор `https|ssh`, multi-host/multi-credential provider profiles, path safety и `job_locks`. | Выполнить generic Git и выбранный managed provider clone requests; проверить transport URL, `repositories.provider`, `repositories.provider_host`, clone в выбранную `target_directory`, несколько provider hosts и credentials на одном provider, path containment и lock serialization. |
| `ACC-MVP-015` | Поддерживаются `environments` и `workspaces`. | Создать/импортировать связи; проверить `GET /api/environments`, `GET /api/workspaces` и FK constraints. |
| `ACC-MVP-016` | `auth` реализован как отдельный модуль. | Проверить `module_states` для `auth` и auth endpoints. |
| `ACC-MVP-017` | Local auth и `RBAC` реализованы на backend/API уровне; SCIM endpoints могут быть contract/stub без полноценного sync workflow. | Проверить users/groups/roles/bindings, bootstrap admin flow, negative authorization tests и controlled SCIM stub responses. |
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
- Для job-producing endpoints проверить, что `Idempotency-Key` scoped by
  `(actor, job_type, key)`: same actor/job type/key replays, different payload
  conflicts, but the same key can be used independently by another job type or
  actor.

### Configuration contract

- Проверить, что `config.example.json` использует `scanning.global_scan`.
- Импортировать config with unknown top-level key; ожидание: `validation_error` без частичного применения.
- Импортировать config with unknown nested key; ожидание: `validation_error` без частичного применения.
- Импортировать config with trailing JSON payload after a valid config object;
  ожидание: `validation_error` без частичного применения.
- Импортировать конфигурацию с `scanning.global_scan`; Stage 02 expectation:
  `config_entries` stores the canonical key and `GET /api/config` returns
  `scanning.global_scan`. Stage 04 materializes scan roots into `root_paths`.
- Импортировать конфигурацию с legacy/alias key `scanning.global_scann`; ожидание: `validation_error` без частичного применения.
- Импортировать конфигурацию с любым другим alias для global scan roots, например `globalScan` или `scan_roots`; ожидание: `validation_error` без частичного применения.
- Проверить, что reload request/result использует ключи вида `scanning.global_scan` и возвращает sync result без Stage 03 `jobs` dependency.
- Отправить reload request с explicit unknown key, например `logging.levl`;
  ожидание: key отражён в `failed_keys`, а не в `accepted_keys` или
  `applied_keys`.
- Проверить, что `PUT /api/config` не удаляет imported system
  `ignore_rules`, так как `.t-helper.ignore` не входит в HTTP config payload.
- Проверить, что storage/API/read models используют `repository_id` для связи project -> repository и не требуют поле `repo_id`.
- Проверить, что `modules.enabled` принимает только registered modules из initial module registry.
- Импортировать конфигурацию с unknown module в `modules.enabled`; ожидание: `validation_error` без частичного применения.
- Импортировать конфигурацию с unknown database provider; ожидание: `validation_error` без частичного применения.
- Проверить, что `database.*` и `external_databases.*` обновляют только storage profile metadata/current bootstrap или `migration` slot и не переключают active DB через reload.
- Проверить, что `thelper-ctl -reconfigure` может обновить `migration` storage profile без изменения `current`.
- Проверить, что `thelper-ctl -migrate-db` переносит schema/data, затем актуализирует `current`/`migration` statuses and preserves old DB profile metadata.
- Проверить SQLite -> PostgreSQL `thelper-ctl -migrate-db` с `external_databases.username/password = secretref://env/...`; ожидание: target DB получает Stage-owned tables/data, secret refs остаются refs, API output masked.
- Проверить, что failed DB migration не меняет active `current` profile.
- Для Stage 02 проверить перенос только Stage 02-owned tables: `config_entries`, `storage_profiles`, `storage_provider_settings`, `module_states`, `ignore_rules` and system migration metadata. Later stage-owned tables must extend this migration test when introduced.
- Проверить, что provider-specific worker settings scoped to PostgreSQL do not change SQLite settings and наоборот.

### Provider adapters

- Проверить, что database providers регистрируются через storage provider registry, а не выбираются условной логикой в HTTP/CLI/domain layers.
- Проверить, что auth providers регистрируются через auth provider registry.
- Проверить, что unknown database/auth provider names возвращают controlled validation errors.
- Проверить, что provider-specific code не используется напрямую из HTTP handlers.
- Проверить, что repository provider adapters нормализуют identity как `provider + provider_host + full_path`.
- Проверить URL parsing по ADR 0016: equivalent HTTPS/SSH/scp-like URLs дают одинаковые `provider`, `provider_host`, `full_path`, `protocol` и safe `clone_url`.
- Проверить provider-specific path rules for MVP provider: GitLab supports nested groups or GitHub requires exactly `owner/repo`; Bitbucket and Azure DevOps path rules are platform/extension tests.
- Проверить negative URL parsing cases: userinfo in persisted URL, unsupported protocol, empty segments, `..`, backslash, provider path shape mismatch.
- Проверить machine-readable URL validation errors: `provider_host_required`, `unsupported_provider_url`, `unsupported_url_protocol`, `invalid_repository_path`, `invalid_provider_host`, `credential_userinfo_not_allowed`, `provider_path_shape_mismatch`.
- Проверить, что одинаковый `full_path` может существовать в разных `provider_host` или разных providers без конфликта unique constraint.
- Проверить, что `clone_url` nullable и не имеет unique constraint.
- Проверить, что `clone_url` не используется для lookup/upsert/deduplication и разные SSH/HTTPS transport URLs для одного repository сходятся в одну repository card.
- Проверить, что persisted `clone_url`, `jobs.payload`, `jobs.result_payload`, `job_events` и logs не содержат credentials, tokens, passwords или URL userinfo.
- Проверить multi-host provider profiles: один provider имеет несколько `repository_provider_instances` с разными `provider_host`.
- Проверить multi-credential per host: один `repository_provider_instance` имеет несколько `repository_credentials` с разными `usages` и `scope_hint`.
- Проверить, что raw credential values отклоняются, а `secretref://env/...` accepted и masked в API responses.
- Проверить usage validation: clone/pull требуют `git_transport`; GitLab recursive group clone and webhook verification are platform/extension tests.
- Проверить, что repo job payloads содержат `credential_id`, но не secret refs и не resolved secrets.

### Repository operation conflicts

- Создать active `repo_clone`, `repo_pull` или `repo_sync` job для `lock_key = repository:<id>`.
- Повторить тот же request с тем же `Idempotency-Key` и тем же payload; ожидание: возвращается существующий `job_id`.
- Повторить request с тем же `Idempotency-Key`, но другим payload; ожидание: `409 Conflict` или `validation_error`.
- Отправить новый `clone`, `pull` или `sync` request без того же `Idempotency-Key`; ожидание: `409 Conflict` с кодом `repository_operation_already_running`.
- Проверить, что conflict details содержат `repository_id`, `lock_key`, `active_job_id` и `active_job_type`.
- Отправить два конкурирующих clone request для одного normalized `provider + provider_host + full_path`, когда `repository_id` ещё не создан; ожидание: pre-create identity conflict prevents duplicate jobs/repository rows, а exact `Idempotency-Key` replay returns the existing `job_ref`.
- Отправить два конкурирующих clone request в один normalized target path для разных repositories; ожидание: второй request получает `409 conflict` with `repository_target_path_busy`.
- Проверить, что MVP не создаёт merged/replacement/cancel/superseding job для конфликтующей repository operation.

### Module registry

- После initial migrations/seed проверить наличие registered modules: `core`, `worker-runtime`, `config-manager`, `module-runtime`, `status-monitor`, `global-scanner`, `repository-manager`, `project-scanner`, `security-validator`, `auth`, `web`.
- Проверить, что `GET /api/modules` возвращает registered modules со state из `module_states`.
- Проверить, что registered module без доступной реализации получает state `unavailable`.
- Проверить, что `POST /api/modules/restart` для unknown module возвращает validation/not found error.
- Проверить, что `POST /api/modules/restart` или reload для `unavailable` module возвращает controlled error без panic и без изменения unrelated module states.

### Secret masking

- Импортировать sensitive config keys со значениями `secretref://env/...`; ожидание: в `config_entries.value` хранится reference, а не resolved secret.
- Вызвать `GET /api/config`; ожидание: sensitive values masked и не раскрывают имя/значение resolved secret сверх разрешённого reference metadata.
- Проверить masking modes: admin with `system.config.read` may see full `secretref://env/NAME`, viewer/runtime summary receives masked metadata without env var name, no response returns resolved value.
- Попробовать импортировать literal secret в sensitive key, включая `external_databases.username` и `external_databases.password`; ожидание: `validation_error` без частичного применения.
- Проверить, что `jobs.payload`, `jobs.result_payload`, `job_events.payload`, `workflow_statuses.summary_payload`, `audit_log.payload` и logs не содержат resolved secrets.

### Authorization

- Проверить, что `GET /api/health` доступен без аутентификации и возвращает только safe metadata без config values, filesystem paths, DSN, users, secrets или object-scoped details.
- Для Stage 01 проверить shape `health_status.v1`: response содержит `instance_id`, `mode`, `database_fingerprint`, `started_at`, `readiness` и `schema_version = health_status.v1`.
- Для каждого write endpoint проверить отказ без требуемого permission.
- Для object-scoped read проверить, что пользователь без object permission не получает sensitive details.
- Для `system.runtime.read` проверить только runtime summary/list metadata, без раскрытия object-scoped details.
- Проверить examples for `system.runtime.read`: counts/module summary allowed; filesystem paths, DSNs, full secret refs, job payloads and object details without object permission denied or masked.

### Local auth password hashing

- Создать local user через setup/API flow; проверить, что raw password не сохраняется в `users` или `local_user_credentials`.
- На пустой auth state запустить `thelper`; проверить, что bootstrap admin user created automatically.
- Проверить, что bootstrap username/password are latin alphanumeric length 16, shown once in first UI and stdout, and warning says credentials expire after 24 hours.
- Проверить, что successful bootstrap login allows creating other users/auth provider settings.
- Проверить, что unused bootstrap user is deleted after 24 hours and is not recreated automatically.
- Проверить, что `local_user_credentials.password_hash` является Argon2id PHC string с production defaults или test-approved override.
- Проверить, что `users` не содержит password hash fields.
- Проверить `POST /api/auth/login` с Argon2id PHC verification.
- Проверить, что successful login создаёт `user_sessions` row с `token_hash`, но raw session token не сохраняется.
- Проверить `GET /api/auth/session` для текущей session и `POST /api/auth/logout`, после которого session недействительна.
- Проверить rehash-on-login: hash со слабыми параметрами обновляется после successful login.
- Проверить failed login generic error без раскрытия существования username.
- Проверить lockout: 5 consecutive failed attempts устанавливают `locked_until` примерно на 15 минут; successful login after unlock сбрасывает `failed_attempt_count`.
- Проверить password reset endpoints: `POST /api/auth/password-reset/request` возвращает generic response, raw token не сохраняется, `password_reset_tokens.token_hash` хранит только hash, token становится invalid после successful `POST /api/auth/password-reset/confirm`, `used_at` или `expires_at`.
- Проверить `POST /api/auth/password/change` для current authenticated local user.
- Проверить, что API responses, `jobs.payload`, `jobs.result_payload`, `job_events.payload`, `workflow_statuses.summary_payload`, `audit_log.payload` и logs не содержат raw passwords, password hashes, session tokens, session token hashes, reset tokens или reset token hashes.

### Bulk write and delete semantics

- These checks validate confirmed MVP behavior from Stage 00 delivery decisions.
- Для MVP bulk `PUT` endpoints проверить non-destructive upsert semantics: omitted records are not deleted.
- Проверить stable identity для upsert: root paths by normalized `path`, provider instances by `provider + provider_host`, credentials by `provider_instance_id + name`, users by `username`, groups by `name`, roles by `name + scope_type`, role bindings by subject/role/scope tuple.
- Проверить, что public `DELETE` endpoints отсутствуют или возвращают documented not found/method not allowed до отдельного API contract.
- Проверить MVP lifecycle без hard delete: root paths/provider records can be disabled/deactivated through documented write fields; projects can become `missing` or `disabled`; repositories can become `disabled`, `missing` or `superseded`; users can become `active = false`.
- Проверить, что UI/API labels and audit actions for MVP lifecycle use disable/deactivate/mark_missing/supersede terminology and do not present these operations as hard delete.

### Payload schemas

- Для каждого `jobs.job_type` проверить наличие `schema_version` в `payload` и `result_payload`.
- Для `project_scans.result_payload` проверить `schema_version = project_scans.result.v1`.
- Для `security_findings.fingerprint_components` проверить `schema_version = security_finding.fingerprint.v1`.
- Проверить, что `security_findings.fingerprint` имеет формат `fp:v1:<sha256>` и соответствует canonical JSON из ADR 0017.
- Проверить, что fingerprint не включает `job_id`, `project_scan_id`, line/column, title, description, remediation или severity.
- Проверить, что `resource_ref` или `finding_key` обязателен для persisted finding.
- Повторить scan с новым `job_id`/`project_scan_id`; ожидание: existing `security_findings` row обновляется по fingerprint, `last_seen_at` меняется, дубль не создаётся.
- Повторить scan с line shift при том же resource/rule; ожидание: fingerprint стабилен.
- Повторить scan с другим `rule_set_id` или `workspace_id`; ожидание: fingerprint отличается.
- Повторить scan after file rename; expectation: fingerprint changes unless adapter has documented stable `finding_key` behavior.
- Проверить workspace-specific finding: same rule/resource in different `workspace_id` produces different fingerprint.
- Проверить finding without `resource_ref`: persisted only when stable non-secret `finding_key` exists.
- Для `job_events.payload` проверить `schema_version = job_events.payload.v1`.
- Для `workflow_statuses.summary_payload` проверить `schema_version = workflow_status.summary.v1`.
- Проверить, что payload/result не содержит секреты, tokens, приватные ключи или raw Terraform source.

### Status aggregation

- Проверить, что каждый worker handler пишет `job_events` для ключевых transitions: `claimed`, `started`, `progress`, `succeeded|failed`.
- Проверить, что `status-monitor` агрегирует jobs одного `job_group_id` в `workflow_statuses`.
- Проверить, что `GET /api/status/workflows/{job_group_id}` возвращает единый aggregate status.
- Проверить, что `GET /api/project-scans/{id}` возвращает aggregate status из `status-monitor`, а не требует агрегации на стороне UI.
- Проверить, что parent `project_scan` job не ждёт child `security_validation_scan` job, но workflow остаётся `running` до завершения child jobs.

### Toolchain coverage

- Проверить `make install` на Linux/macOS: TFLint 0.63.1 и Trivy 0.71.2 загружаются, checksum manifests и SHA-256 архивов проверяются, binaries устанавливаются рядом с t-helper.
- Проверить admission boundary Terraform: `1.14.x` отклоняется, `1.15.0` и более новые версии принимаются bundled profile.
- Для `project-scanner` проверить, что runtime flow включает `terraform validate` и `TFLint`.
- Для `security-validator` проверить, что `Trivy` запускается только при наличии в `project_security_scan_settings.enabled_modules`.
- Проверить, что `terraform`, `TFLint` и `Trivy` запускаются через tool profile runtime из ADR 0018, а не через parser logic внутри scanner service.
- Проверить version discovery для каждого required tool и выбор active `tool_profiles` по `tool`, discovered version and configured version policy.
- Проверить default `certified_only` policy: certified version runs, unsupported version returns controlled `tool_version_unsupported`, missing binary returns `tool_not_found`.
- Проверить `compatible_range` policy: compatible but uncertified version may run and result payload marks `compatibility_status = compatible`, `certification_status = uncertified`.
- Проверить `latest_best_effort` policy requires explicit opt-in and marks results as uncertified.
- Проверить, что `jobs.project_scan.result.v1`, `jobs.security_validation_scan.result.v1` and `project_scans.result.v1` include tool/profile metadata and do not include raw tool output.
- Проверить profile validation fixtures: sample stdout/stderr plus expected normalized DTO produce stable normalized results and expected fingerprint components.
- Проверить negative profile fixtures: missing required fields, unsupported schema, parse failure and secret-like values produce controlled validation errors or redacted diagnostics.
- Проверить, что tool profile files cannot contain shell fragments, arbitrary scripts, eval behavior or commands outside explicit version discovery and scan command templates.
- Проверить migration to fresh tool version without code changes: add/update profile file, validate fixtures, import, activate, run scan and confirm normalized DTO/fingerprint stability.
- Проверить `tool-profile-analyzer`: captured output can produce a candidate profile and validation fixtures, but generated profiles use `source_type = generated_candidate`, are inactive by default and are never selected until explicit validation and activation.
- Проверить analyzer diagnostics for fingerprint-affecting fields: unresolved `rule_id`, normalized file path, `resource_ref`, `finding_key`, `rule_namespace`, `tool` or `check_type` blocks activation until fixtures validate them.
- Проверить bundled profile fixtures для `terraform validate` success/error, `TFLint` finding, `Trivy config` finding, secret-like redaction, unsupported version, missing binary and malformed output.
- Для extension adapters проверить, что `gitleaks`, `checkov`, `opa` и `conftest` могут быть зарегистрированы в `scanning.security_scan.modules` без изменения API contract, but are not mandatory MVP acceptance.
- Перед закрытием Stage 06B запустить `THELPER_STAGE06_REAL_TOOLCHAIN=1 go test -run TestStage06RealCertifiedToolchain ./internal/scanner` с сертифицированными версиями `terraform`, `tflint` и `trivy` внутри окружения без network access; skipped test не считается приёмкой.

### Storage invariants

- Проверить unique constraints из `data-model.md`.
- Проверить, что migrations are stage-owned: Stage 01 does not create target
  tables for later stages without the owning code/API/worker behavior and tests.
- Проверить, что `repositories.provider + repositories.provider_host + repositories.full_path` unique, а `repositories.full_path` не unique globally.
- Проверить FK/delete behavior для `projects`, `workspaces`, `project_scans`, `job_locks`, `role_bindings` и `scim_identities`.
- Проверить, что expired `job_locks` не блокируют новые operations.
- Проверить, что logical migration versions синхронизированы между supported dialect directories.
- Для Stage 01 storage contract suite запускается на SQLite и PostgreSQL.
- Для Stage 10 тот же storage contract suite запускается на MySQL и MSSQL.
- Запустить PostgreSQL storage contract suite against Aurora PostgreSQL writer endpoint before declaring Aurora PostgreSQL supported.
- Запустить MySQL storage contract suite against Aurora MySQL writer endpoint before declaring Aurora MySQL supported.
- Проверить, что migrations и runtime не требуют superuser privileges on Aurora PostgreSQL.
- Проверить, что Aurora MySQL tests используют InnoDB-compatible schema assumptions.
- Проверить, что Babelfish for Aurora PostgreSQL не проходит как `mssql` adapter target без отдельного compatibility decision.

Implemented baseline coverage:

- `make test` runs `gofmt` check, `go vet ./...` and `go test ./...`;
- shared storage contract tests run for SQLite in every `go test ./...` run;
- the same storage contract suite runs for PostgreSQL when
  `THELPER_POSTGRES_DSN` is set, including the Docker `offline` test runner and
  GitHub Actions;
- PostgreSQL storage tests guard destructive cleanup and require a test database
  name unless `THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS=1` is set;
- tests assert that Stage 01 migrations do not create later-stage tables;
- tests assert synchronized logical migration versions for SQLite/PostgreSQL;
- tests assert idempotent Stage 01 migration application.

### Worker execution

- Проверить, что API/CLI создают jobs в статусе `queued`, но не выполняют long-running operations inline.
- Запустить отдельный `thelper-worker` и проверить atomic claim + transition `queued -> running -> succeeded|failed`.
- Запустить `thelper-worker` с теми же storage provider/DSN settings, что
  `thelper`, и проверить, что он применяет migrations, resolves active storage
  profile and consumes jobs from the active database.
- Проверить, что `leased_by` и worker diagnostics используют формат `<hostname>:<pid>:<worker_uuid>`.
- Запустить несколько worker-процессов и проверить, что `job_locks` не допускают конфликтующие операции с одинаковым `lock_key`.
- Запустить несколько worker-процессов на одном queued job и проверить, что lease получает только один worker.
- Проверить heartbeat update для long-running job: `jobs.heartbeat_at` and
  `jobs.lease_expires_at` update on ticks, while `job_events` heartbeat rows are
  bounded diagnostics and are not required for every tick.
- Проверить expired lease recovery: job возвращается в `queued` с `run_after` или становится `failed` после исчерпания `max_attempts`.
- Проверить retry/backoff через `attempt_count`, `max_attempts` и `run_after`.
- Проверить default retry policy: `max_attempts = 3`, initial backoff `5s`, multiplier `2`, max backoff `5m`, jitter в допустимых пределах.
- Проверить lock contention: worker не запускает handler, очищает lease, возвращает job в `queued`, выставляет `run_after` и пишет `job_events`.
- Проверить retention cleanup: old `job_events` и released/expired `job_locks` старше retention удаляются, active jobs/locks и `audit_log` не удаляются.
- Проверить, что остановка worker-процесса не останавливает `thelper` API runtime.
- Для SQLite проверить effective `workers.concurrency = 1`, one active worker process via database-fingerprint worker lock, `journal_mode = WAL`, `foreign_keys = ON`, configured `busy_timeout`, rejection `sqlite_worker_concurrency_unsupported` when applying higher concurrency to active SQLite profile, and stale worker lock replacement after the original process is gone.
- Проверить, что enqueue rejects `jobs.payload` with secret-like JSON keys, URL userinfo or unresolved `secretref://...` values before persistence.
- Для PostgreSQL проверить, что provider-specific concurrency может быть выше SQLite without changing SQLite provider settings.

### Singleton runtime

- Запустить `thelper`, затем открыть Tauri GUI; ожидание: GUI подключается к существующему runtime.
- Запустить Tauri GUI без активного `thelper`; ожидание: GUI запускает local `thelper`, а `Web UI` подключается к этому же runtime.
- Попробовать запустить второй `thelper`; ожидание: процесс обнаруживает существующий runtime и не создаёт второй активный экземпляр.
- Проверить runtime lock file: содержит `instance_id`, `pid`, `host`, `api_listen_address`, `started_at`, `updated_at`, `config_database_fingerprint`.
- Проверить `/api/health`: возвращает тот же `health_status.v1` DTO shape, что и Stage 01, без breaking schema change.
- Проверить, что runtime lock `config_database_fingerprint` совпадает с safe `/api/health.database_fingerprint`.
- Проверить stale lock replacement: если PID отсутствует и health probe fails, новый runtime заменяет stale lock.
- Проверить fail-closed behavior: если PID и health probe дают противоречивое состояние, второй runtime не стартует и возвращает диагностическую ошибку.

### Frontend UI contract

- Проверить, что `Web UI` и `GUI` используют route tree из `docs/ru/frontend-ui-contract.md`.
- Проверить, что route, доступный в `Web UI`, доступен в `GUI` для того же release scope, unless explicitly marked local-only or unavailable by backend API scope.
- Проверить, что frontend clients используют один typed API client and documented backend API only.
- Проверить, что list-heavy screens are compact and table-first: projects, repositories, findings, jobs, modules, audit and auth administration lists.
- Проверить, что object detail pages use content headers and tabs for subviews instead of unrelated page shells.
- Проверить, что long-running operations show `job_id`, status, latest event and links to job/status details.
- Проверить, что `GUI` uses runtime lock file plus `GET /api/health` for local discovery/readiness and authenticated `/api/status` for detailed runtime state.
- Проверить, что Tauri packaging/signing policy and update/distribution channel policy are documented before release artifact publication.

### Scanner fixtures

- `basic_tf_project`: директория с `main.tf`.
- `nested_tf_project`: parent с `main.tf` и nested `child/main.tf`.
- `git_repo_marker_directory`: `.git/` directory.
- `git_repo_marker_file`: `.git` regular file для worktree/submodule, первая непустая строка начинается с `gitdir:`.
- `git_repo_marker_file_invalid`: `.git` regular file без первой непустой строки `gitdir:`.
- `git_marker_negative_gitignore`: `.gitignore` без `.git/` и без `.git` file.
- `git_marker_negative_gitattributes`: `.gitattributes` без `.git/` и без `.git` file.
- `git_marker_negative_gitmodules`: `.gitmodules` без `.git/` и без `.git` file.
- `git_marker_negative_github_dir`: `.github/` без `.git/` и без `.git` file.
- `git_marker_negative_gitlab_ci`: `.gitlab-ci.yml` без `.git/` и без `.git` file.
- `git_marker_negative_gitkeep`: `.gitkeep` без `.git/` и без `.git` file.
- `ignored_directory`: директория, исключённая `.t-helper.ignore`.
- `negative_ignore_pattern`: rule `!pattern`, сохранённая без применения в exclude-only MVP matcher.
- `symlinked_directory`: symlinked directory skipped when `follow_symlinks = false`.

### Stage 04 scanner registry behavior

- Проверить, что global scanner не открывает `*.tf` файлы для discovery и использует только directory entry names.
- Проверить, что global scanner creates/updates only `projects` and enqueues `project_discovery` jobs, then continues traversal without waiting for them.
- Проверить, что `.git` regular file читается только `project_discovery` job до implementation limit и валидируется по первой непустой строке `gitdir:`.
- Проверить, что global scanner не читает `.git`, `.git/config` и не вызывает `git` CLI.
- Проверить `follow_symlinks = false`: symlinked directories skipped, `directories_skipped` and `symlinks_skipped` updated.
- Проверить filesystem-only Git repository fallback identity from project discovery: `provider = generic`, `provider_host = local`, `root_path_id = <containing root path>`, `full_path = <root_path-relative path>`, `clone_url = null`; одинаковые `full_path` в разных root paths не должны merge в одну repository card.
- Проверить same-repository relationship: multiple local Terraform projects in one Git repository remain separate `projects` rows, share `repository_id` when known, and get `project_links.link_type = same_repository`.
- Проверить idempotent upsert keys: repeated scan updates `projects.last_seen_at` by `root_path_id + relative_path` and does not create duplicate `repositories` for the same generic local path.
- Проверить missing behavior: previously discovered project absent from completed scan gets `projects.status = missing` and is not deleted.
- Проверить default project listing: `GET /api/projects` без `status` возвращает только `active`, `status=missing|disabled|all` возвращает соответствующие non-active records.
- Проверить missing project scan guard: `POST /api/project-scans` for `projects.status = missing` returns controlled validation error.
- Проверить rediscovery: later global scan of same `root_path_id + relative_path` changes `projects.status` from `missing` to `active`.
- Проверить progressive field population: global scan fills only filesystem registry fields, project discovery fills `repository_id`/`project_links`, repository manager enriches repository card, and project scan data is not written as summary fields on `projects`.
- Проверить partial directory errors: per-directory error writes `job_events`, increments `errors_count`, and job may still finish `succeeded` when at least one root path was processed successfully.
- Проверить all-root failure: when every requested root path fails before useful traversal, job finishes `failed`.
- Проверить MVP symlink contract: requests/config attempting `follow_symlinks = true` are rejected with controlled validation error until Stage 09 runtime hardening.

### Stage 05 repository enrichment

- Проверить enrich in place: generic `provider = generic`, `provider_host = local` repository gets provider-aware identity and keeps the same `repositories.id` when no provider-aware card exists.
- Проверить relink and supersede: when provider-aware repository card already exists, projects are relinked to it and generic repository becomes `status = superseded` with `superseded_by_repository_id`.
- Проверить, что project rows are never merged during repository enrichment.
- Проверить, что `superseded` repository rejects clone/pull/sync/webhook/polling operations with controlled validation error.
- Проверить default repository listing: `GET /api/repos` without `status` returns only `active`, while explicit `status` filters can include `superseded`, `missing` or `disabled`.

### Stage 05 path safety

- Проверить rejection for `../` in `target_directory`, `new_target_directory`, repository name and provider path.
- Проверить symlink inside root cannot make `local_path` escape selected `root_path`.
- Проверить case-insensitive collision on case-insensitive filesystem.
- Проверить Unicode normalization before uniqueness and containment checks.
- Проверить existing empty directory accepted only after containment/conflict checks.
- Проверить existing non-empty non-Git directory returns controlled validation error.
- Проверить existing Git repository with expected remote turns clone into pull.
- Проверить existing Git repository with different remote rejects clone.

### API compatibility

- Проверить, что `GET /api/scans/{job_id}` returns global scan job during MVP.
- Проверить, что new frontend client code uses canonical `GET /api/jobs/{id}` or status endpoints.
- Проверить, что compatibility endpoint removal requires documented deprecation cycle.
