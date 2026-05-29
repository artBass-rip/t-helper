# T-Helper Architecture

## Purpose

`t-helper` is a modular on-premise system for:

- discovering Terraform working directories in the filesystem;
- registering projects and Git repositories in persistent storage;
- local analysis of Terraform code without SaaS and without sending source code outside the installation;
- managing configuration, users, access and modules through the `GUI`, `Web UI` and `thelper-ctl`.

## Architectural Principles

- the database is the source of truth for runtime configuration and working entities;
- `config.json` and `.t-helper.ignore` are used only for initial import through `thelper-ctl -reconfigure`;
- `GUI` and `Web UI` use a single backend API; feature parity is required only for read/operate scenarios included in a specific release;
- the Frontend MVP covers the main read/operate scenarios and full MVP administrative screens; Stage 12 is responsible for hardening and platform-only administrative extensions where they are in release scope;
- every module must have a clear lifecycle and support independent restart;
- all external providers, including database/storage and auth providers, are implemented as separate pluggable modules or libraries behind stable internal interfaces;
- monolithic and distributed modes must use a compatible interaction model;
- Terraform code, findings and security rules do not leave the installation perimeter.

## Technology Stack

The canonical implementation stack is described in [`technology-stack.md`](technology-stack.md).

- Backend runtime, CLI, API, module lifecycle, jobs framework and storage adapters are implemented in `Go`.
- `Web UI` is implemented with `React`, `TypeScript`, `Vite`, `TanStack Router`, `TanStack Query`, `Zod`, `React Hook Form` and `Ant Design`.
- The local `GUI` is implemented with `Tauri` and uses the same React/TypeScript codebase as the `Web UI`.
- Stage 08 route map, navigation model, operational density and Tauri delivery policy are defined in [`frontend-ui-contract.md`](frontend-ui-contract.md).

## Terraform Project in the MVP

A Terraform project is a directory that contains at least one `*.tf` file.

MVP constraints:

- directories that contain only `.terraform`, `*.tfstate`, or `*.tfvars` without `*.tf` are not considered projects;
- after a Terraform project is discovered, traversal below that directory stops;
- `terragrunt.hcl` support is considered an extension for later versions.

## Project Discovery and Git Marker Allowlist in the MVP

`global-scanner` discovers only Terraform working directories in the blocking traversal path and registers separate `projects` rows. For each created or updated project it enqueues a background `project_discovery` job and continues the scan without waiting for the result.

`project_discovery` treats a directory as a Git working tree only when one of the allowed markers is present:

- `.git/` directory;
- `.git` regular file for a worktree/submodule when the first non-empty line starts with `gitdir:`.

Files and directories such as `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` and similar convention/config files are not Git markers in the MVP.

`global-scanner` does not read `.git` metadata and does not shell out to `git`. `project_discovery` may read a `.git` file only as Git metadata, not as Terraform source. The MVP limits the readable `.git` file size to `4 KiB` and is not required to resolve the path after `gitdir:`.

If several local Terraform projects belong to one Git repository, project records are not merged. They remain separate `projects` rows and are linked through a shared `repository_id` and `project_links.link_type = same_repository`.

## Logical Modules

- `core` - orchestration, API, lifecycle, logging, jobs framework
- `worker-runtime` - separate `thelper-worker` worker processes that execute queued jobs from persistent storage
- `status-monitor` - logical module for aggregating job events, workflow statuses, worker health, module health and runtime metrics
- `global-scanner` - traversal of global `root_path` entries from `scanning.global_scan`, ignore matcher, Terraform project discovery and enqueueing background `project_discovery` jobs
- `project-scanner` - project-level Terraform scan using a specific project's settings; baseline toolchain: `terraform validate`, `TFLint`
- `security-validator` - security/validation scan using a specific project's settings and available modules from `scanning.security_scan.modules`; MVP requires `Trivy` as the mandatory local scanner; other tools are adapter extensions
- `tool-profile-runtime` - shared execution/compatibility layer for external CLI tools; performs version discovery, profile selection, command execution, output parsing and normalized DTO mapping according to ADR 0018
- `tool-profile-analyzer` - optional maintainer/operator tool for generating candidate profile versions from captured external tool outputs and validation fixtures; generated profiles require explicit validation and activation
- `repository-manager` - `clone`, `pull`, `sync`, protection from conflicting operations; webhook/polling operations are delivered as later extensions
- `config-manager` - import, validation and application of configuration from the database
- `auth` - authentication providers, `SCIM`, `RBAC`, users, groups, roles, bindings
- `module-runtime` - module states, `reload`, `restart`
- `web` - `Web UI` delivery
- `gui` - desktop client for local use
- `nginx` - reverse proxy, TLS termination, static delivery

## Provider adapters

Provider-specific integrations are pluggable modules or libraries:

- database/storage providers: `sqlite`, `postgresql`, `mysql`, `mssql`;
- auth providers: local auth and future external IAM/SSO providers;
- SCIM providers;
- repository providers: `gitlab`, `github`, `bitbucket`, `azure_devops` and generic Git where explicitly supported;
- policy/tool providers where integration behavior is provider-specific.

Provider-specific code must not live in HTTP handlers, CLI commands or domain logic. Runtime services select providers through configuration and provider registries.

## HTTP API composition

Executable HTTP routes are registered through a compile-time
`httpapi.RouteRegistrar` contract. Each handler owns its route declarations via
`RegisterRoutes(chi.Router)`, while `httpapi.New` owns only router creation,
common middleware and composition.

This keeps route ownership close to handler behavior and prevents unsupported
handler types from being silently ignored. The current route surface is guarded
by `internal/httpapi/router_test.go`.

The accepted decision is documented in
[`adr/0019-code-quality-and-route-registration.md`](adr/0019-code-quality-and-route-registration.md).

## Deployment Scenarios

### Monolithic Mode

The following run on one host:

- `core`
- `worker-runtime` / `thelper-worker`
- `status-monitor`
- `global-scanner`
- `project-scanner`
- `security-validator`
- `repository-manager`
- `config-manager`
- `auth`
- `module-runtime`
- `web`
- `nginx`

`GUI` runs locally on an administrator or user workstation.

Only one `t-helper` runtime instance is active in a single local installation:

- if `thelper` is already running for the `Web UI`, the Tauri GUI connects to the existing local runtime;
- if `thelper` is not running yet, the Tauri GUI starts a local `thelper`, after which the `Web UI` connects to the same runtime;
- a repeated `thelper` start must discover the existing runtime through the lock/health mechanism and must not create a second active process.

### Distributed Mode

The following can be moved out separately:

- `global-scanner`
- `project-scanner`
- `security-validator`
- `repository-manager`
- `worker-runtime` / `thelper-worker`
- `status-monitor`
- `auth`
- `web`
- `nginx`
- database

## Storage strategy

Target storage engines on the roadmap:

- `SQLite`
- `PostgreSQL`
- `MySQL`
- `MSSQL`

Default values:

- `SQLite` - default internal SQL-like storage for local setup
- `PostgreSQL` - default external storage for server setup
- `MySQL` - optional external storage
- `MSSQL` - optional external storage

Managed engine compatibility:

- Aurora PostgreSQL uses the `PostgreSQL` storage adapter and migration dialect.
- Aurora MySQL uses the `MySQL` storage adapter and migration dialect.
- Babelfish for Aurora PostgreSQL is not treated as the `MSSQL` adapter target unless a separate compatibility decision is accepted.

Storage layer requirements:

- the logical data model is the same for all backends;
- the backend uses a storage abstraction;
- SQL storage engines support dialect-specific migrations with synchronized logical migration versions;
- the internal database is selected through `database.database_type` and `database.database_path`;
- if `external_databases.enabled = true`, the runtime uses `external_databases` and does not use the internal database for working data;
- `SQLite` and `PostgreSQL` are included in the first mandatory implementation stage;
- `MySQL`, `MSSQL` and other SQL-compatible adapters are delivered as separate adapters in Stage 10 Storage Adapter Expansion.
- transaction boilerplate should be centralized with `storage.WithTx` in new
  or materially changed store paths, while domain-specific retry/isolation
  remains explicit in the store code;
- shared SQL dialect helpers are a documented optimization backlog item before
  Stage 10 expands storage adapters.

## Jobs and Worker Execution

Background jobs are executed by separate worker processes.

Rules:

- `thelper` creates jobs, validates requests, manages API/config/module state and does not execute long-running jobs inline;
- `thelper-worker` atomically claims an eligible queued job through a storage-level lease, moves it to `running`, executes the job handler, stores `result_payload` and completes the job;
- worker processes update `heartbeat_at` and `lease_expires_at` for long-running jobs;
- expired leases are recovered by worker processes through retry/backoff or by moving the job to `failed` after attempts are exhausted;
- `job_locks` are used to serialize conflicting business operations across multiple worker processes;
- the job lease identifies the owner of a specific job, while `job_locks` protect business resources;
- `global-scanner`, `project-scanner`, `security-validator`, `repository-manager` and `scim_sync` run through the worker execution model;
- worker processes can scale horizontally while preserving unified storage contracts.

## Status Monitoring and Aggregation

All jobs must publish status events and runtime metrics to persistent `job_events`.

`status-monitor`:

- aggregates `job_events`, `jobs`, `module_states` and worker heartbeat data;
- owns aggregate read models `workflow_statuses`;
- updates aggregate `project_scans.status` and `project_scans.result_payload`;
- provides the single source of status information for the UI, backend API and internal services;
- in the MVP, may be a logical module inside `thelper`, but must have a separate responsibility boundary;
- in distributed mode, may be moved to a separate process or node.

State ownership rule:

- workers write execution facts and domain results;
- `status-monitor` writes aggregate statuses;
- UI and internal services must not aggregate workflow status from child jobs on their own.

### Repo storage strategy

The same materialized set of `root_paths` used for global scanning is used for
local placement of cloned repositories. `scanning.global_scan` remains the
external config source for `source = config` rows; root paths created through
clone/API are stored as `source = api` and do not rewrite config.

Rules:

- during clone, the user selects an existing `root_path` or creates a new root path;
- inside the selected root path, the user selects an existing target directory or creates a new one;
- the clone workflow uses provider-aware adapters for `gitlab`, `github`, `bitbucket`, `azure_devops`;
- provider integrations use GitKraken-like profiles: one provider can have multiple configured hosts/provider instances, and one host can have multiple credentials with different usages;
- protocol `https|ssh` is selected at clone request level and affects the final clone URL;
- repository identity is built as `provider + provider_host + full_path`, where `provider_host` distinguishes self-hosted provider instances;
- provider URL parsing follows ADR 0016 and must normalize equivalent HTTPS/SSH/scp-like URLs into the same identity;
- `clone_url` is stored only as nullable non-unique transport metadata without credentials/userinfo and does not participate in deduplication;
- credentials are selected by `credential_id`; workers resolve `secretref://...` values at use time and never receive raw secrets in job payloads;
- recursive GitLab group clone is a later repository operations extension after single-repository clone;
- if clone runs into a new path, `repository-manager` adds it to the `root_paths` list with `source = api`;
- the local repository path is built inside the selected `root_path`;
- `provider_host`, `full_path` and the resulting `local_path` must be normalized; `local_path` must not allow escaping the selected `root_path`;
- if the directory already contains a valid Git repository with the expected remote, `clone` is replaced with `pull`;
- `clone`, `pull` and `sync` operations for the same `repository.id` are serialized through `job_locks`.

## Basic Runtime Flow

1. `thelper-ctl -reconfigure` imports configuration and ignore rules into the database.
2. `thelper` starts the runtime and reads the active configuration from the database.
3. `global-scanner` starts a global scan over `root_path` entries from `scanning.global_scan`.
4. Discovered Terraform projects are registered as separate `projects` rows; a background `project_discovery` job is created for each created or updated project.
5. `project_discovery` determines the project's local Git relationships, creates/updates a repository card when a Git marker exists, and links separate project records through `project_links` if they belong to one Git repository.
6. `repository-manager` supports `clone`, `pull`, `sync`; when cloning into a new path it adds that path to `root_paths`.
7. Stage 06A provides `tool-profile-runtime`, certified profiles, profile validation and optional analyzer for external CLI compatibility.
8. `project-scanner` performs a project-level Terraform scan using a specific project's settings and `terraform validate` and `TFLint` through `tool-profile-runtime`.
9. `security-validator` performs a security/validation scan using a specific project's settings and `Trivy` through `tool-profile-runtime`, then stores normalized findings. `Gitleaks`, `Checkov`, `OPA` and `Conftest` are connected as adapter extensions.
10. `module-runtime` provides `reload`, `restart` and module statuses.

## Key Architectural Risks Removed by Documentation Refactoring

- dependency on line-number references in another document was removed;
- product requirements and technical mechanics were separated into different documents;
- duplicates and inconsistent wording were consolidated into a single source of truth;
- the repository received a stable structure for subsequent code scaffolding.
