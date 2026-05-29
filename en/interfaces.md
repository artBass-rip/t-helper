# CLI, API and Configuration

## Executable Components

- `thelper` - main runtime and backend service, implemented in `Go`
- `thelper-worker` - separate worker process for executing queued jobs, implemented in `Go`
- `thelper-ctl` - CLI for configuration import, lifecycle and administrative operations, implemented in `Go`

The frontend stack for the `Web UI` and local `GUI` is defined in [`technology-stack.md`](technology-stack.md).

## Required `thelper-ctl` Commands

- `thelper-ctl -reconfigure`
- `thelper-ctl -reload`
- `thelper-ctl -restart <module>`
- `thelper-ctl -migrate-db`

## Recommended Commands

- `thelper-ctl scan start`
- `thelper-ctl scan status`
- `thelper-ctl project-scan start <project>`
- `thelper-ctl project-scan status <project|job>`
- `thelper-ctl repos clone <url>`
- `thelper-ctl repos pull <project|repo>`
- `thelper-ctl repos sync <project|repo>`
- `thelper-ctl tool-profiles validate <path>`
- `thelper-ctl tool-profiles import <path>`
- `thelper-ctl tool-profiles activate <tool> <profile_id> <profile_version>`
- `thelper-ctl tool-profiles analyze <samples_path> --baseline <profile_id>`
- `thelper-ctl modules list`

## Current implemented backend API

Current Stage 02 executable API:

- `GET /api/health`
- `GET /api/config`
- `PUT /api/config`
- `GET /api/modules`
- `POST /api/modules/reload`
- `POST /api/modules/restart`

## Target MVP backend API

HTTP conventions, response schemas and the endpoint skeleton are described in [`api.md`](api.md).

- `GET /api/health`
- `GET /api/root-paths`
- `PUT /api/root-paths`
- `GET /api/projects`
- `GET /api/projects/{id}`
- `GET /api/projects/{id}/scan-settings`
- `PUT /api/projects/{id}/scan-settings`
- `GET /api/environments`
- `GET /api/environments/{id}`
- `GET /api/workspaces`
- `GET /api/workspaces/{id}`
- `POST /api/scans`
- `GET /api/scans/{job_id}` - temporary compatibility endpoint for global scan jobs; canonical status endpoint is `GET /api/jobs/{id}`
- `POST /api/project-scans`
- `GET /api/project-scans/{project_scan_id}`
- `GET /api/project-scans/{project_scan_id}/findings`
- `GET /api/jobs`
- `GET /api/jobs/{id}`
- `GET /api/status`
- `GET /api/status/workflows`
- `GET /api/status/workflows/{job_group_id}`
- `GET /api/status/jobs/{job_id}`
- `GET /api/status/workers`
- `GET /api/config`
- `PUT /api/config`
- `GET /api/ignore-rules`
- `PUT /api/ignore-rules`
- `GET /api/repos`
- `GET /api/repos/{id}`
- `GET /api/repo-provider-instances`
- `PUT /api/repo-provider-instances`
- `GET /api/repo-credentials`
- `PUT /api/repo-credentials`
- `POST /api/repos/clone`
- `POST /api/repos/pull`
- `POST /api/repos/sync`
- `GET /api/security/findings`
- `GET /api/security/findings/{id}`
- `GET /api/security/rule-sets`
- `PUT /api/security/rule-sets`
- `GET /api/tool-profiles`
- `POST /api/tool-profiles/validate`
- `POST /api/tool-profiles/import`
- `POST /api/tool-profiles/activate`
- `POST /api/tool-profiles/analyze`
- `GET /api/modules`
- `POST /api/modules/reload`
- `POST /api/modules/restart`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/session`
- `POST /api/auth/password-reset/request`
- `POST /api/auth/password-reset/confirm`
- `POST /api/auth/password/change`
- `GET /api/auth/users`
- `PUT /api/auth/users`
- `GET /api/auth/groups`
- `PUT /api/auth/groups`
- `GET /api/auth/roles`
- `PUT /api/auth/roles`
- `GET /api/auth/role-bindings`
- `PUT /api/auth/role-bindings`
- `GET /api/auth/scim/identities`
- `POST /api/auth/scim/sync`
- `GET /api/audit`

### API decisions

- `POST /api/project-scans` is the single endpoint for starting a project-level Terraform scan and security/validation checks attached to the project.
- Project scan starts as a parent-child workflow: the parent `project_scan` job creates child `security_validation_scan` jobs when security modules are enabled; all jobs are linked through `job_group_id`.
- `status-monitor` aggregates workflow/job statuses, while the UI and internal services read aggregate status through documented status/project scan endpoints.
- Security findings are read through `/api/security/findings` or through the scoped endpoint `/api/project-scans/{project_scan_id}/findings`.
- A separate `/api/security/scans` endpoint is not introduced, to avoid duplicating `project_scans` in the data model.
- `GET /api/health` is a confirmed safe unauthenticated endpoint for local singleton runtime discovery and does not expose config, paths, DSN, secrets, users or object-scoped details.
- Runtime auth endpoints (`login`, `logout`, `session`, password reset/change) are separated from administrative auth endpoints (`users`, `groups`, `roles`, `role-bindings`, `SCIM`).
- Write endpoints that start background operations create `jobs` and return `job_id`. Stage 02 config/module lifecycle endpoints are explicit synchronous exceptions until an async variant or documented contract migration is introduced; Stage 03 job-backed handlers exist for framework validation and future workflow integration without changing those public sync contracts.
- Write endpoints that change reference data or configuration update entities idempotently where applicable.
- Confirmed MVP behavior: bulk `PUT` endpoints are non-destructive idempotent upserts by stable identity or `id`; omitted records are not deleted.
- Confirmed MVP behavior: public `DELETE` endpoints are out of scope for MVP even though delete permissions are seeded for future lifecycle expansion.
- Confirmed MVP lifecycle behavior: administrative removal is represented by explicit non-destructive state transitions such as disabling/deactivating records, marking projects missing or superseding repositories. Hard delete requires a future explicit API contract.
- Repository operations must use `job_locks` to serialize `clone`, `pull` and `sync` for one `repository.id`.
- Stage 05 MVP conflict policy: if a `queued` or `running` `clone`, `pull` or `sync` job already exists for `lock_key = repository:<id>`, a new repository operation request returns `409 conflict` with code `repository_operation_already_running`, except for an exact replay with the same `Idempotency-Key`, which returns the existing `job_ref`. `clone` additionally checks normalized repository identity and normalized target path conflicts before a stable `repository_id` exists.
- Long-running operations are executed by separate `thelper-worker` processes; the backend API creates jobs and does not execute them inline.
- `GET/PUT /api/repo-provider-instances` manages GitKraken-like integration profiles for cloud/on-premise/multi-domain Git hosts.
- `GET/PUT /api/repo-credentials` manages multi-credential records per provider host; API accepts only `secretref://...`, and raw secrets are rejected.
- `POST /api/repos/clone` must accept a provider-aware request: `provider_instance_id` or `provider` + optional/derived `provider_host`, optional `credential_id`, selection of `root_path_id` or `new_root_path`, selection of existing `target_directory` or `new_target_directory`, and explicit `protocol = https|ssh`.
- Repository identity is normalized as `provider + provider_host + full_path`; `clone_url` is nullable, non-unique and is not used for lookup/upsert/deduplication.
- Persisted `clone_url` must be safe-normalized and must not contain credentials, tokens, passwords or URL userinfo.
- Provider URL parsing for `gitlab`, `github`, `bitbucket`, `azure_devops` follows ADR 0016; equivalent HTTPS/SSH/scp-like URLs must produce the same identity.
- For MVP, clone/pull credentials must support `git_transport`. For GitLab recursive group clone, the credential must support usage `provider_api`, and for webhook verification, `webhook`; these workflows ship after Stage 05 MVP.
- The UI clone workflow must show the protocol selector next to the URL field.
- For `provider = gitlab`, a separate `clone_scope = gitlab_group_recursive` mode must be supported.

## Configuration Model

After the initial import, the source of truth is:

- Database

Initial import files:

- `config.json`
- `.t-helper.ignore`

`config.example.json` is shipped as a valid reference for the structure of `config.json`.

Logical configuration sections:

- `system_settings`
- `database`
- `external_databases`
- `scanning`
- `repositories`
- `security`
- `api`
- `auth`
- `workers`
- `modules`
- `logging`

The detailed structure and validation rules are described in [`configuration.md`](configuration.md).

Project-level scan and security/validation scan are configured per project. In global configuration, `scanning.security_scan.modules` defines only the list of security/validation modules and policy engines available for attachment to a project. Base stack: Stage 06A ships the tool profile runtime; Stage 06B `project-scanner` uses `terraform validate` and `TFLint`, and `security-validator` uses `Trivy` as the mandatory scanner; `Gitleaks`, `Checkov`, `OPA`/`Conftest` are attached as adapter extensions.

### Base Scanning Stack

- `global-scanner` - discovery-only module without reading raw Terraform source beyond what is necessary for discovery.
- `project-scanner` - orchestration layer for `terraform validate` and `TFLint`.
- `security-validator` - orchestration layer for the mandatory MVP `Trivy` scanner and future scanner adapters.
- `OPA`/`Conftest` - optional policy engines for enterprise rule sets after the MVP scanner contract.

Minimum keys for `repositories`:

- `default_auth_type`
- `poll_interval_default`
- `auto_sync_default`

## Configuration Behavior

### `thelper-ctl -reconfigure`

The command must:

- read `config.json`;
- read `.t-helper.ignore`;
- validate input data through the strict schema contract;
- reject unknown keys, deprecated aliases and sensitive literal values;
- accept only `scanning.global_scan` for global scan roots;
- atomically write configuration and ignore rules to the database;
- not start the service;
- not restart the service directly.

### `thelper-ctl -reload`

The command must:

- initiate rereading configuration from the database;
- apply reloadable parameters without a full restart;
- explicitly report parameters that require restart.

### `thelper-ctl -restart <module>`

The command must:

- restart any individual module;
- work with an extensible module list;
- update module state in `module_states`.

## Global Scan Algorithm

1. Get active `root_path`, imported from `scanning.global_scan`, and ignore rules from the database.
2. Build the exclusion matcher.
3. Start directory traversal.
4. Check ignore rules for each directory.
5. Read only the list of names at the current level.
6. If `*.tf` is found, create or update a separate Terraform project record, enqueue a `project_discovery` job and do not descend below it.
7. Continue traversal only through allowed subdirectories without waiting for `project_discovery`.

Global scan does not determine Git repository identity and does not register a Git repository directly. The Git marker allowlist (`.git/` directory or `.git` file whose first non-empty line is `gitdir:`) is applied by the background `project_discovery` job. Do not treat these as Git markers: `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` and other similar convention/config files. Global scan and `project_discovery` must not shell out to `git`.

## Scanner Optimizations

- use directory entry API;
- do not open `*.tf` when the file name is enough;
- cache the ignore matcher for the job duration;
- traverse symlinks only in an explicitly enabled mode.
