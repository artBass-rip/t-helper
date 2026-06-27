# Test Plan

This document links MVP acceptance criteria from [`roadmap.md`](roadmap.md) to
API checks, storage state and runtime behavior.

## General Rules

- Acceptance tests must run against a local on-premise environment without external SaaS dependencies.
- API assertions use endpoint contracts from [`api.md`](api.md).
- Payload assertions use schema versions from [`payload-schemas.md`](payload-schemas.md).
- Storage assertions use entities and invariants from [`data-model.md`](data-model.md).
- Authorization assertions use permissions from [`access-control.md`](access-control.md).
- Test Terraform fixtures must not contain real secrets.

## MVP Acceptance Mapping

| ID | Acceptance | Primary checks |
| --- | --- | --- |
| `ACC-MVP-001` | A Terraform project is discovered by the presence of `*.tf`. | Create a fixture with `main.tf`; run `POST /api/scans`; verify `projects` with `terraform_marker = *.tf`. |
| `ACC-MVP-002` | Global scanning identifies Terraform projects, and background project discovery identifies project Git relationships. | Fixture contains `*.tf` and allowed Git markers `.git/` directory and `.git` file with `gitdir:`; verify `projects`, queued `project_discovery` jobs, `repositories`/`project_links` after project discovery completes; verify negative fixtures for similar files that must not create repository links/cards. |
| `ACC-MVP-003` | After a project is discovered, nested directories are not scanned as separate working directories. | Fixture contains parent `main.tf` and nested `child/main.tf`; verify that no second `project` exists under the parent. |
| `ACC-MVP-004` | Ignore rules exclude files and directories; `!pattern` is preserved. | Load `.t-helper.ignore`; verify `ignore_rules.pattern`, excluded directories and preserved negative patterns. |
| `ACC-MVP-005` | Projects are stored as separate records and updated in the database; project Git relationships are stored without merging project records. | Run scan twice; verify no duplicates by unique constraints and `last_seen_at` update; run project discovery and verify shared `repository_id`/`project_links` for projects from one Git repository without merging project rows. |
| `ACC-MVP-006` | Project-level scan identifies providers, required auth and quality through `terraform validate` and `TFLint`, while security/validation scan stores findings through `Trivy` as the mandatory local scanner. | Configure `PUT /api/projects/{id}/scan-settings`, run `POST /api/project-scans`; verify parent `jobs.job_type = project_scan`, child `jobs.job_type = security_validation_scan` with the same `job_group_id`, `project_scans.result_payload`, `workflow_statuses` and `security_findings` for `Trivy`. |
| `ACC-MVP-007` | Runtime configuration is stored in the database. | Import config; verify `config_entries` and `GET /api/config`. |
| `ACC-MVP-008` | `thelper-ctl -reconfigure` imports configuration and ignore rules. | Run the CLI command; verify `config_entries`, `ignore_rules` and no service restart side effect. |
| `ACC-MVP-009` | `thelper-ctl -reload` applies reloadable configuration. | Change `modules.enabled`, `logging.level` or `scanning.global_scan`; verify sync reload result with `accepted_keys`, `applied_keys`, `restart_required_keys`, `failed_keys`, no Stage 03 `jobs` dependency and honest distinction between accepted and actually applied Stage 02 keys. |
| `ACC-MVP-010` | `thelper-ctl -restart <module>` works for any available individual module, and unavailable modules return a controlled error. | Restart an available module such as `config-manager` or `global-scanner`; verify `module_states` transition and `module_restart.result.v1`; restart/reload of unavailable roadmap modules must return controlled `module_unavailable`. |
| `ACC-MVP-011` | `GUI` and `Web UI` use a unified backend API and cover MVP read/operate scenarios. | Contract test: both frontend clients use only the documented backend API. |
| `ACC-MVP-012` | `GUI` works only locally. | Verify bind policy/config and rejection of remote GUI access. |
| `ACC-MVP-013` | `PostgreSQL` and `SQLite` are supported through storage abstraction. | Run the same storage test suite against PostgreSQL and SQLite adapters. |
| `ACC-MVP-014` | `clone`, `pull`, `sync` are serialized through `job_locks`, clone uses the shared root path, and a new root path is added automatically to root paths. | Run concurrent repo operations; verify one `held` lock and no conflicting active jobs; run `POST /api/repos/clone` with `new_root_path` and verify the new `root_path`; verify clone into existing and new `target_directory`; verify repository identity as `provider + provider_host + full_path`. |
| `ACC-MVP-022` | Clone workflow supports `generic` Git and one managed provider from `gitlab` or `github`, `https|ssh` selection, multi-host/multi-credential provider profiles, path safety and `job_locks`. | Run generic Git and selected managed provider clone requests; verify transport URL, `repositories.provider`, `repositories.provider_host`, clone into selected `target_directory`, multiple provider hosts and credentials on one provider, path containment and lock serialization. |
| `ACC-MVP-015` | `environments` and `workspaces` are supported. | Create/import relationships; verify `GET /api/environments`, `GET /api/workspaces` and FK constraints. |
| `ACC-MVP-016` | `auth` is implemented as a separate module. | Verify `module_states` for `auth` and auth endpoints. |
| `ACC-MVP-017` | Local auth and `RBAC` are implemented at backend/API level; SCIM endpoints may be contract/stub without a full sync workflow. | Verify users/groups/roles/bindings, bootstrap admin flow, negative authorization tests and controlled SCIM stub responses. |
| `ACC-MVP-018` | The security stack works locally and does not send code outside. | Run project scan in a network-restricted environment; verify no outbound calls. |
| `ACC-MVP-019` | Security findings and rule sets are stored inside the system. | Verify `security_rule_sets`, `security_findings`, `GET /api/security/findings`. |
| `ACC-MVP-020` | One project scan API creates `project_scans`; findings are read without `security_scans`. | Verify `POST /api/project-scans`, `GET /api/project-scans/{project_scan_id}/findings`; confirm that there is no separate `security_scans` entity. |
| `ACC-MVP-021` | Backend API covers scan roots, repositories, jobs, environments, workspaces and module states for the Frontend MVP. | Contract test against the endpoint list from `api.md` and the authorization matrix. |

## Cross-Cutting Tests

### Idempotency

- Repeat a write request with the same `Idempotency-Key` and same payload.
- Expected: the same `job_id` or the same resulting state is returned.
- Repeat a write request with the same `Idempotency-Key` but different payload.
- Expected: `409 Conflict` or `validation_error` with an explicit code.
- For job-producing endpoints, verify that `Idempotency-Key` is scoped by
  `(actor, job_type, key)`: same actor/job type/key replays, different payload
  conflicts, but the same key can be used independently by another job type or
  actor.

### Configuration Contract

- Verify that `config.example.json` uses `scanning.global_scan`.
- Import config with unknown top-level key; expected: `validation_error` without partial application.
- Import config with unknown nested key; expected: `validation_error` without partial application.
- Import config with trailing JSON payload after a valid config object; expected: `validation_error` without partial application.
- Import config with `scanning.global_scan`; Stage 02 expectation: `config_entries` stores the canonical key and `GET /api/config` returns `scanning.global_scan`. Stage 04 materializes scan roots into `root_paths`.
- Import config with legacy/alias key `scanning.global_scann`; expected: `validation_error` without partial application.
- Import config with any other alias for global scan roots, for example `globalScan` or `scan_roots`; expected: `validation_error` without partial application.
- Verify that reload request/result uses keys such as `scanning.global_scan` and returns a sync result without a Stage 03 `jobs` dependency.
- Send reload request with an explicit unknown key, for example `logging.levl`; expected: key appears in `failed_keys`, not in `accepted_keys` or `applied_keys`.
- Verify that `PUT /api/config` does not delete imported system `ignore_rules`, because `.t-helper.ignore` is not part of the HTTP config payload.
- Verify that storage/API/read models use `repository_id` for the project -> repository relationship and do not require `repo_id`.
- Verify that `modules.enabled` accepts only registered modules from the initial module registry.
- Import config with an unknown module in `modules.enabled`; expected: `validation_error` without partial application.
- Import config with an unknown database provider; expected: `validation_error` without partial application.
- Verify that `database.*` and `external_databases.*` update only storage profile metadata/current bootstrap or the `migration` slot and do not switch the active DB through reload.
- Verify that `thelper-ctl -reconfigure` can update the `migration` storage profile without changing `current`.
- Verify that `thelper-ctl -migrate-db` copies schema/data, then updates `current`/`migration` statuses and preserves old DB profile metadata.
- Verify SQLite -> PostgreSQL `thelper-ctl -migrate-db` with `external_databases.username/password = secretref://env/...`; expected: target DB receives stage-owned tables/data, secret refs remain refs, API output is masked.
- Verify that a failed DB migration does not change the active `current` profile.
- For Stage 02, verify transfer only of Stage 02-owned tables: `config_entries`, `storage_profiles`, `storage_provider_settings`, `module_states`, `ignore_rules` and system migration metadata. Later stage-owned tables must extend this migration test when introduced.
- Verify that provider-specific worker settings scoped to PostgreSQL do not change SQLite settings and vice versa.

### Provider Adapters

- Verify that database providers are registered through the storage provider registry, not selected by conditional logic in HTTP/CLI/domain layers.
- Verify that auth providers are registered through the auth provider registry.
- Verify that unknown database/auth provider names return controlled validation errors.
- Verify that provider-specific code is not used directly from HTTP handlers.
- Verify that repository provider adapters normalize identity as `provider + provider_host + full_path`.
- Verify URL parsing according to ADR 0016: equivalent HTTPS/SSH/scp-like URLs produce the same `provider`, `provider_host`, `full_path`, `protocol` and safe `clone_url`.
- Verify provider-specific path rules for the MVP provider: GitLab supports nested groups or GitHub requires exactly `owner/repo`; Bitbucket and Azure DevOps path rules are platform/extension tests.
- Verify negative URL parsing cases: userinfo in persisted URL, unsupported protocol, empty segments, `..`, backslash, provider path shape mismatch.
- Verify machine-readable URL validation errors: `provider_host_required`, `unsupported_provider_url`, `unsupported_url_protocol`, `invalid_repository_path`, `invalid_provider_host`, `credential_userinfo_not_allowed`, `provider_path_shape_mismatch`.
- Verify that the same `full_path` can exist in different `provider_host` values or different providers without a unique constraint conflict.
- Verify that `clone_url` is nullable and has no unique constraint.
- Verify that `clone_url` is not used for lookup/upsert/deduplication and that different SSH/HTTPS transport URLs for one repository converge to one repository card.
- Verify that persisted `clone_url`, `jobs.payload`, `jobs.result_payload`, `job_events` and logs do not contain credentials, tokens, passwords or URL userinfo.
- Verify multi-host provider profiles: one provider has several `repository_provider_instances` with different `provider_host`.
- Verify multi-credential per host: one `repository_provider_instance` has several `repository_credentials` with different `usages` and `scope_hint`.
- Verify that raw credential values are rejected, while `secretref://env/...` is accepted and masked in API responses.
- Verify usage validation: clone/pull require `git_transport`; GitLab recursive group clone and webhook verification are platform/extension tests.
- Verify that repo job payloads contain `credential_id`, but not secret refs or resolved secrets.

### Repository Operation Conflicts

- Create an active `repo_clone`, `repo_pull` or `repo_sync` job for `lock_key = repository:<id>`.
- Repeat the same request with the same `Idempotency-Key` and same payload; expected: existing `job_id` is returned.
- Repeat the request with the same `Idempotency-Key` but different payload; expected: `409 Conflict` or `validation_error`.
- Send a new `clone`, `pull` or `sync` request without the same `Idempotency-Key`; expected: `409 Conflict` with code `repository_operation_already_running`.
- Verify that conflict details contain `repository_id`, `lock_key`, `active_job_id` and `active_job_type`.
- Send two concurrent clone requests for one normalized `provider + provider_host + full_path` before `repository_id` exists; expected: pre-create identity conflict prevents duplicate jobs/repository rows, and exact `Idempotency-Key` replay returns the existing `job_ref`.
- Send two concurrent clone requests to one normalized target path for different repositories; expected: second request receives `409 conflict` with `repository_target_path_busy`.
- Verify that the MVP does not create merged/replacement/cancel/superseding jobs for a conflicting repository operation.

### Module Registry

- After initial migrations/seed, verify registered modules: `core`, `worker-runtime`, `config-manager`, `module-runtime`, `status-monitor`, `global-scanner`, `repository-manager`, `project-scanner`, `security-validator`, `auth`, `web`.
- Verify that `GET /api/modules` returns registered modules with state from `module_states`.
- Verify that a registered module without an available implementation gets state `unavailable`.
- Verify that `POST /api/modules/restart` for an unknown module returns validation/not found error.
- Verify that `POST /api/modules/restart` or reload for an `unavailable` module returns a controlled error without panic and without changing unrelated module states.

### Secret Masking

- Import sensitive config keys with values `secretref://env/...`; expected: `config_entries.value` stores the reference, not the resolved secret.
- Call `GET /api/config`; expected: sensitive values are masked and do not expose resolved secret name/value beyond allowed reference metadata.
- Verify masking modes: admin with `system.config.read` may see full `secretref://env/NAME`, viewer/runtime summary receives masked metadata without env var name, no response returns a resolved value.
- Try importing a literal secret into a sensitive key, including `external_databases.username` and `external_databases.password`; expected: `validation_error` without partial application.
- Verify that `jobs.payload`, `jobs.result_payload`, `job_events.payload`, `workflow_statuses.summary_payload`, `audit_log.payload` and logs do not contain resolved secrets.

### Authorization

- Verify that `GET /api/health` is available without authentication and returns only safe metadata without config values, filesystem paths, DSNs, users, secrets or object-scoped details.
- For Stage 01, verify `health_status.v1` shape: response contains `instance_id`, `mode`, safe `database_fingerprint`, `started_at`, `readiness` and `schema_version`.
- Verify that every endpoint in the authorization matrix in `access-control.md` has a positive and negative permission test.
- Verify that `system.runtime.read` can read aggregate runtime/list metadata but does not expose object-scoped details without corresponding object permissions.
- Verify that object-scoped `*.read` permissions allow reading only the matching object scope.
- Verify that group role bindings are inherited by group members.
- Verify that local auth failures use generic errors and do not reveal whether a username exists.
- Verify that password hashes, reset tokens and reset token hashes are never returned by API, logs, jobs, events, workflow summaries or audit payloads.

### Payload Schemas and Findings

- Verify `jobs.payload` and `jobs.result_payload` schema versions for every job type.
- For `project_scans.result_payload`, verify `schema_version = project_scans.result.v1`.
- For `security_findings.fingerprint_components`, verify `schema_version = security_finding.fingerprint.v1`.
- Verify that `security_findings.fingerprint` has format `fp:v1:<sha256>` and matches canonical JSON from ADR 0017.
- Verify that fingerprint does not include `job_id`, `project_scan_id`, line/column, title, description, remediation or severity.
- Verify that `resource_ref` or `finding_key` is required for a persisted finding.
- Repeat scan with a new `job_id`/`project_scan_id`; expected: existing `security_findings` row is updated by fingerprint, `last_seen_at` changes and no duplicate is created.
- Repeat scan with a line shift for the same resource/rule; expected: fingerprint is stable.
- Repeat scan with another `rule_set_id` or `workspace_id`; expected: fingerprint differs.
- Repeat scan after file rename; expected: fingerprint changes unless adapter has documented stable `finding_key` behavior.
- Verify workspace-specific finding: same rule/resource in different `workspace_id` produces different fingerprint.
- Verify finding without `resource_ref`: persisted only when stable non-secret `finding_key` exists.
- For `job_events.payload`, verify `schema_version = job_events.payload.v1`.
- For `workflow_statuses.summary_payload`, verify `schema_version = workflow_status.summary.v1`.
- Verify that payload/result does not contain secrets, tokens, private keys or raw Terraform source.

### Status Aggregation

- Verify that every worker handler writes `job_events` for key transitions: `claimed`, `started`, `progress`, `succeeded|failed`.
- Verify that `status-monitor` aggregates jobs with one `job_group_id` into `workflow_statuses`.
- Verify that `GET /api/status/workflows/{job_group_id}` returns a single aggregate status.
- Verify that `GET /api/project-scans/{id}` returns aggregate status from `status-monitor`, not requiring UI-side aggregation.
- Verify that parent `project_scan` job does not wait for child `security_validation_scan` job, but the workflow remains `running` until child jobs finish.

### Toolchain Coverage

- Verify `make install` on Linux/macOS: TFLint 0.63.1 and Trivy 0.71.2 are downloaded, checksum manifests and archive SHA-256 values are verified, and binaries are installed next to t-helper.
- Verify the Terraform admission boundary: `1.14.x` is rejected while `1.15.0` and newer versions are accepted by the bundled profile.
- For `project-scanner`, verify that runtime flow includes `terraform validate` and `TFLint`.
- For `security-validator`, verify that `Trivy` runs only when present in `project_security_scan_settings.enabled_modules`.
- Verify that `terraform`, `TFLint` and `Trivy` run through the tool profile runtime from ADR 0018, not through parser logic inside scanner service.
- Verify version discovery for each required tool and selection of active `tool_profiles` by `tool`, discovered version and configured version policy.
- Verify default `certified_only` policy: certified version runs, unsupported version returns controlled `tool_version_unsupported`, missing binary returns `tool_not_found`.
- Verify `compatible_range` policy: compatible but uncertified version may run and result payload marks `compatibility_status = compatible`, `certification_status = uncertified`.
- Verify `latest_best_effort` policy requires explicit opt-in and marks results as uncertified.
- Verify that `jobs.project_scan.result.v1`, `jobs.security_validation_scan.result.v1` and `project_scans.result.v1` include tool/profile metadata and do not include raw tool output.
- Verify profile validation fixtures: sample stdout/stderr plus expected normalized DTO produce stable normalized results and expected fingerprint components.
- Verify negative profile fixtures: missing required fields, unsupported schema, parse failure and secret-like values produce controlled validation errors or redacted diagnostics.
- Verify that tool profile files cannot contain shell fragments, arbitrary scripts, eval behavior or commands outside explicit version discovery and scan command templates.
- Verify migration to a fresh tool version without code changes: add/update profile file, validate fixtures, import, activate, run scan and confirm normalized DTO/fingerprint stability.
- Verify `tool-profile-analyzer`: captured output can produce a candidate profile and validation fixtures, but generated profiles use `source_type = generated_candidate`, are inactive by default and are never selected until explicit validation and activation.
- Verify analyzer diagnostics for fingerprint-affecting fields: unresolved `rule_id`, normalized file path, `resource_ref`, `finding_key`, `rule_namespace`, `tool` or `check_type` blocks activation until fixtures validate them.
- Verify bundled profile fixtures for `terraform validate` success/error, `TFLint` finding, `Trivy config` finding, secret-like redaction, unsupported version, missing binary and malformed output.
- For extension adapters, verify that `gitleaks`, `checkov`, `opa` and `conftest` can be registered in `scanning.security_scan.modules` without changing the API contract, but are not mandatory MVP acceptance.
- Before closing Stage 06B, run `THELPER_STAGE06_REAL_TOOLCHAIN=1 go test -run TestStage06RealCertifiedToolchain ./internal/scanner` with the certified `terraform`, `tflint` and `trivy` versions inside a network-disabled environment; a skipped test does not count as acceptance.

### Storage Invariants

- Verify unique constraints from `data-model.md`.
- Verify that migrations are stage-owned: Stage 01 does not create target
  tables for later stages without the owning code/API/worker behavior and tests.
- Verify that `repositories.provider + repositories.provider_host + repositories.full_path` is unique, while `repositories.full_path` is not globally unique.
- Verify FK/delete behavior for `projects`, `workspaces`, `project_scans`, `job_locks`, `role_bindings` and `scim_identities`.
- Verify that expired `job_locks` do not block new operations.
- Verify that logical migration versions are synchronized across supported dialect directories.
- For Stage 01, storage contract suite runs on SQLite and PostgreSQL.
- For Stage 10, the same storage contract suite runs on MySQL and MSSQL.
- Run PostgreSQL storage contract suite against Aurora PostgreSQL writer endpoint before declaring Aurora PostgreSQL supported.
- Run MySQL storage contract suite against Aurora MySQL writer endpoint before declaring Aurora MySQL supported.
- Verify that migrations and runtime do not require superuser privileges on Aurora PostgreSQL.
- Verify that Aurora MySQL tests use InnoDB-compatible schema assumptions.
- Verify that Babelfish for Aurora PostgreSQL does not pass as an `mssql` adapter target without a separate compatibility decision.

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

### Worker Execution

- Verify that API/CLI create jobs in `queued` status but do not execute long-running operations inline.
- Start a separate `thelper-worker` and verify atomic claim plus transition `queued -> running -> succeeded|failed`.
- Start `thelper-worker` with the same storage provider/DSN settings as
  `thelper`, and verify that it applies migrations, resolves the active storage
  profile and consumes jobs from the active database.
- Verify that `leased_by` and worker diagnostics use format `<hostname>:<pid>:<worker_uuid>`.
- Start several worker processes and verify that `job_locks` prevent conflicting operations with the same `lock_key`.
- Start several worker processes on one queued job and verify that only one worker receives the lease.
- Verify heartbeat update for a long-running job: `jobs.heartbeat_at` and
  `jobs.lease_expires_at` update on ticks, while `job_events` heartbeat rows are
  bounded diagnostics and are not required for every tick.
- Verify expired lease recovery: job returns to `queued` with `run_after` or becomes `failed` after `max_attempts` is exhausted.
- Verify retry/backoff through `attempt_count`, `max_attempts` and `run_after`.
- Verify default retry policy: `max_attempts = 3`, initial backoff `5s`, multiplier `2`, max backoff `5m`, jitter within allowed bounds.
- Verify lock contention: worker does not start the handler, clears the lease, returns the job to `queued`, sets `run_after` and writes `job_events`.
- Verify retention cleanup: old `job_events` and released/expired `job_locks` older than retention are deleted, while active jobs/locks and `audit_log` are not deleted.
- Verify that stopping a worker process does not stop the `thelper` API runtime.
- For SQLite, verify effective `workers.concurrency = 1`, one active worker process via database-fingerprint worker lock, `journal_mode = WAL`, `foreign_keys = ON`, configured `busy_timeout`, rejection `sqlite_worker_concurrency_unsupported` when applying higher concurrency to active SQLite profile, and stale worker lock replacement after the original process is gone.
- Verify that enqueue rejects `jobs.payload` with secret-like JSON keys, URL userinfo or unresolved `secretref://...` values before persistence.
- For PostgreSQL, verify that provider-specific concurrency can be higher than SQLite without changing SQLite provider settings.

### Singleton Runtime

- Start `thelper`, then open Tauri GUI; expected: GUI connects to the existing runtime.
- Start Tauri GUI without active `thelper`; expected: GUI starts local `thelper`, and `Web UI` connects to the same runtime.
- Try to start a second `thelper`; expected: the process discovers the existing runtime and does not create a second active instance.
- Verify runtime lock file: contains `instance_id`, `pid`, `host`, `api_listen_address`, `started_at`, `updated_at`, `config_database_fingerprint`.
- Verify `/api/health`: returns the same `health_status.v1` DTO shape as Stage 01, without breaking schema change.
- Verify that runtime lock `config_database_fingerprint` matches safe `/api/health.database_fingerprint`.
- Verify stale lock replacement: if PID is absent and health probe fails, a new runtime replaces the stale lock.
- Verify fail-closed behavior: if PID and health probe give contradictory state, the second runtime does not start and returns a diagnostic error.

### Frontend UI Contract

- Verify that `Web UI` and `GUI` use the route tree from `docs/en/frontend-ui-contract.md`.
- Verify that a route available in `Web UI` is available in `GUI` for the same release scope, unless explicitly marked local-only or unavailable by backend API scope.
- Verify that frontend clients use one typed API client and documented backend API only.
- Verify that list-heavy screens are compact and table-first: projects, repositories, findings, jobs, modules, audit and auth administration lists.
- Verify that object detail pages use content headers and tabs for subviews instead of unrelated page shells.
- Verify that long-running operations show `job_id`, status, latest event and links to job/status details.
- Verify that `GUI` uses runtime lock file plus `GET /api/health` for local discovery/readiness and authenticated `/api/status` for detailed runtime state.
- Verify that Tauri packaging/signing policy and update/distribution channel policy are documented before release artifact publication.

### Scanner Fixtures

- `basic_tf_project`: directory with `main.tf`.
- `nested_tf_project`: parent with `main.tf` and nested `child/main.tf`.
- `git_repo_marker_directory`: `.git/` directory.
- `git_repo_marker_file`: `.git` regular file for worktree/submodule, first non-empty line starts with `gitdir:`.
- `git_repo_marker_file_invalid`: `.git` regular file without first non-empty line `gitdir:`.
- `git_marker_negative_gitignore`: `.gitignore` without `.git/` and without `.git` file.
- `git_marker_negative_gitattributes`: `.gitattributes` without `.git/` and without `.git` file.
- `git_marker_negative_gitmodules`: `.gitmodules` without `.git/` and without `.git` file.
- `git_marker_negative_github_dir`: `.github/` without `.git/` and without `.git` file.
- `git_marker_negative_gitlab_ci`: `.gitlab-ci.yml` without `.git/` and without `.git` file.
- `git_marker_negative_gitkeep`: `.gitkeep` without `.git/` and without `.git` file.
- `ignored_directory`: directory excluded by `.t-helper.ignore`.
- `negative_ignore_pattern`: rule `!pattern`, preserved without application in the exclude-only MVP matcher.
- `symlinked_directory`: symlinked directory skipped when `follow_symlinks = false`.

### Stage 04 Scanner Registry Behavior

- Verify that global scanner does not open `*.tf` files for discovery and uses only directory entry names.
- Verify that global scanner creates/updates only `projects` and enqueues `project_discovery` jobs, then continues traversal without waiting for them.
- Verify that `.git` regular file is read only by the `project_discovery` job up to the implementation limit and is validated by the first non-empty line `gitdir:`.
- Verify that global scanner does not read `.git`, `.git/config` and does not invoke the `git` CLI.
- Verify `follow_symlinks = false`: symlinked directories skipped, `directories_skipped` and `symlinks_skipped` updated.
- Verify filesystem-only Git repository fallback identity from project discovery: `provider = generic`, `provider_host = local`, `root_path_id = <containing root path>`, `full_path = <root_path-relative path>`, `clone_url = null`; identical `full_path` values in different root paths must not merge into one repository card.
- Verify same-repository relationship: multiple local Terraform projects in one Git repository remain separate `projects` rows, share `repository_id` when known, and get `project_links.link_type = same_repository`.
- Verify idempotent upsert keys: repeated scan updates `projects.last_seen_at` by `root_path_id + relative_path` and does not create duplicate `repositories` for the same generic local path.
- Verify missing behavior: previously discovered project absent from completed scan gets `projects.status = missing` and is not deleted.
- Verify default project listing: `GET /api/projects` without `status` returns only `active`, while `status=missing|disabled|all` returns corresponding non-active records.
- Verify missing project scan guard: `POST /api/project-scans` for `projects.status = missing` returns controlled validation error.
- Verify rediscovery: later global scan of same `root_path_id + relative_path` changes `projects.status` from `missing` to `active`.
- Verify progressive field population: global scan fills only filesystem registry fields, project discovery fills `repository_id`/`project_links`, repository manager enriches repository card, and project scan data is not written as summary fields on `projects`.
- Verify partial directory errors: per-directory error writes `job_events`, increments `errors_count`, and job may still finish `succeeded` when at least one root path was processed successfully.
- Verify all-root failure: when every requested root path fails before useful traversal, job finishes `failed`.
- Verify MVP symlink contract: requests/config attempting `follow_symlinks = true` are rejected with controlled validation error until Stage 09 runtime hardening.

### Stage 05 Repository Enrichment

- Verify enrich in place: generic `provider = generic`, `provider_host = local` repository gets provider-aware identity and keeps the same `repositories.id` when no provider-aware card exists.
- Verify relink and supersede: when provider-aware repository card already exists, projects are relinked to it and generic repository becomes `status = superseded` with `superseded_by_repository_id`.
- Verify that project rows are never merged during repository enrichment.
- Verify that `superseded` repository rejects clone/pull/sync/webhook/polling operations with controlled validation error.
- Verify default repository listing: `GET /api/repos` without `status` returns only `active`, while explicit `status` filters can include `superseded`, `missing` or `disabled`.

### Stage 05 Path Safety

- Verify rejection for `../` in `target_directory`, `new_target_directory`, repository name and provider path.
- Verify symlink inside root cannot make `local_path` escape selected `root_path`.
- Verify case-insensitive collision on case-insensitive filesystem.
- Verify Unicode normalization before uniqueness and containment checks.
- Verify existing empty directory accepted only after containment/conflict checks.
- Verify existing non-empty non-Git directory returns controlled validation error.
- Verify existing Git repository with expected remote turns clone into pull.
- Verify existing Git repository with different remote rejects clone.

### API Compatibility

- Verify that `GET /api/scans/{job_id}` returns global scan job during the MVP.
- Verify that new frontend client code uses canonical `GET /api/jobs/{id}` or status endpoints.
- Verify that compatibility endpoint removal requires a documented deprecation cycle.
