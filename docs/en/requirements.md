# Requirements

## Functional Requirements

### Global Scanning

The system must:

- run scans against one or more `root_path` entries from `scanning.global_scan`;
- find Terraform projects by the presence of `*.tf`;
- avoid reading Terraform file contents when names are sufficient for discovery;
- stop descending after a Terraform working directory is discovered;
- create or update a separate `projects` record for every locally discovered Terraform working directory;
- enqueue a background `project_discovery` job for every created or updated project and continue the filesystem scan without waiting for that job;
- use `follow_symlinks = false` as the only supported MVP traversal mode; extended symlink traversal is deferred to Stage 09 runtime hardening;
- support manual runs, scheduled runs and internal-event runs;
- run as a separate `global-scanner` module.

### Project Discovery and Git Relationships

The system must:

- run a background `project_discovery` job for an individual project;
- determine whether a project is part of a Git working tree only in `project_discovery`, not in the blocking path of the global scan;
- find Git repositories only by the MVP Git marker allowlist: `.git/` directory or `.git` regular file for worktrees/submodules;
- for a `.git` file, read only the Git metadata file and treat the marker as valid when the first non-empty line starts with `gitdir:`;
- not treat `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` or similar files/directories as Git markers;
- not merge different project records even when they belong to one Git repository;
- mark projects from one Git repository as related through `project_links.link_type = same_repository` and a shared `repository_id` when the repository is already known.

### Scan Exclusions

The system must:

- support a `.gitignore`-like rule format;
- apply rules relative to `root_path`;
- support glob masks for files and directories;
- import rules from `.t-helper.ignore`;
- store and edit rules through the database and frontend;
- preserve negative `!pattern` rules without data loss and apply them after full `.gitignore` semantics are implemented.

### Object Registration

After discovery, the system must:

- create or update a Terraform project card;
- populate the project card progressively: fields unknown during the global scan remain nullable/default and are updated by later discovery/scanning/management stages;
- create or update a Git repository card only within background project discovery or repository-manager workflows;
- record first and last discovery time;
- not delete project records when a path disappears; when a project is absent from a repeated completed scan, move it to `status = missing`;
- automatically return the project record to `status = active` when the same `root_path_id + relative_path` is found again;
- preserve relationships with `repository`, `environment` and `workspace` when known.

### Lifecycle Without Hard Delete in the MVP

The MVP does not provide public hard delete endpoints. Deletion in user
scenarios is represented through explicit non-destructive states:

- root paths and provider/configuration records disable/deactivate through
  `enabled = false` or an equivalent active-state field;
- projects use `status = missing` for missing paths and `status = disabled`
  for administrative disablement;
- repositories use `status = disabled`, `missing` or `superseded`;
- users use `active = false`;
- omitted records in bulk `PUT` requests are never deleted.

Future hard delete/archive endpoints require a separate API contract,
permissions, migration behavior and tests.

### Repository Operations

The system must support on the roadmap:

- `clone`
- `pull`
- `sync`
- webhook-based sync
- polling-based sync

The MVP repository-manager supports `clone`, `pull` and `sync` for `generic`
Git and one managed provider from `gitlab` or `github`. Polling sync, recursive
GitLab group clone, `bitbucket` and `azure_devops` adapters are deferred to
repository extension stages, and webhook sync is deferred to Stage 14.

Constraints:

- clone and scan must use the same materialized list of `root_paths`;
- the user must be able to select an existing root path imported from `scanning.global_scan` or created through the API as the target path for clone;
- the user must be able to create a new root path during clone;
- a new root path created during clone must be saved automatically in `root_paths` with `source = api`; `scanning.global_scan` remains an external config source and is not rewritten by `repository-manager`;
- inside the selected root path, the user must be able to select an existing directory for clone or create a new one;
- the local repository path must be inside the selected root path and selected target directory;
- in the UI, the protocol for clone must be selected next to the URL input and accept `https` or `ssh`;
- different providers must be supported for clone on the roadmap: `gitlab`, `github`, `bitbucket`, `azure_devops`; the MVP includes `generic` Git and one managed provider from `gitlab` or `github`;
- integration UX must support cloud and on-premise/multi-domain provider hosts: GitHub, GitHub Enterprise Server, GitLab, GitLab Self-Managed, Bitbucket, Bitbucket Data Center and Azure DevOps;
- one provider must support multiple hosts/provider instances;
- one provider host must support multiple credentials with different usages/permissions;
- the provider must affect URL parsing, transport protocol selection and bulk operations;
- repository identity must be defined as `provider + provider_host + full_path` to support multiple providers and self-hosted instances;
- `clone_url` must be treated as a nullable non-unique transport endpoint, not as an identity key or deduplication key;
- persisted `clone_url` must not contain credentials, tokens, passwords or URL userinfo;
- repository operations must accept an explicit `credential_id` or use the repository default credential;
- credentials must store only `secretref://...` references, not raw secret values;
- cloning one repository and a separate bulk clone workflow must be supported;
- bulk clone of all projects in a GitLab group with recursive traversal of all nested subgroups is deferred until after the single-repository clone MVP;
- if the local directory already contains a valid Git repository, `pull` is performed instead of another `clone`;
- `SSH` and `HTTPS` must be supported;
- conflicting parallel operations on one repository must be blocked.

### Baseline Scanning Stack

Module roles and locally executed tools must be fixed by documentation and
runtime configuration:

- `global-scanner` is responsible for Terraform project discovery, project registry updates and enqueueing background `project_discovery` jobs;
- `project-scanner` uses `terraform validate` and `TFLint` for project-level Terraform checks;
- `security-validator` uses `Trivy` as the mandatory MVP scanner; `Gitleaks`, `Checkov`, `OPA` and `Conftest` are connected as extensions;
- enterprise-policy checks are connected through `OPA` and `Conftest`;
- findings, rules and policies remain local and are not sent to external services.

### Project-Level Terraform Scanning

Project-level scan is started manually, on a schedule, after `clone`, after
`pull` or by an internal event.

Project-level scan settings are defined relative to an individual project. The
global configuration must not contain default project scan settings.

Project-level scan runs as a separate `project-scanner` module.

Technology baseline for `project-scanner`:

- `terraform validate`
- `TFLint`

Minimum check set:

- `terraform.providers`
- `terraform.required_auth`
- `terraform.syntax`
- `terraform.deprecations`
- `terraform.quality`
- `terraform.module_source`
- `terraform.provider_usage`
- `terraform.policy`

### Security Scanning and Code Validation

Security/validation scan runs relative to an individual project and is executed
by a separate `security-validator` module.

Technology roadmap baseline for `security-validator`:

- `Trivy`
- `Gitleaks`
- `Checkov`

Enterprise-policy checks must be supported through:

- `OPA`
- `Conftest`

MVP requires `Trivy` as the mandatory local security scanner. `Gitleaks`,
`Checkov`, `OPA` and `Conftest` are adapter extension points and are not
mandatory MVP acceptance.

The system must:

- store only the list of available check modules and policy engines in global configuration `scanning.security_scan.modules`;
- allow available security/validation modules to be enabled in project-specific settings;
- not run a check module for a project unless it is enabled in that project's settings;
- run `terraform.validate`, `terraform.secrets`, `terraform.security.misconfig`, `terraform.policy` and extensible security/validation modules;
- store findings linked to the project, rule set, job and discovery time.

Expected result:

- list of providers, their versions and aliases;
- metadata about required authorization;
- findings for syntax, deprecations, quality and security;
- fingerprinted results linked to rule set and discovery time.

### Configuration and Lifecycle

The system must:

- store runtime configuration in the database;
- validate configuration before writing;
- apply reloadable settings through `thelper-ctl -reload`;
- support `thelper-ctl -restart <module>` for any individual module;
- show module states through UI and CLI.
- execute background jobs in separate `thelper-worker` processes, not inline inside the API runtime.

### Frontend

Both interfaces, `GUI` and `Web UI`, must use a unified backend API. The MVP
requires the main read/operate scenarios:

- viewing projects, environments and workspaces;
- managing scan roots and ignore rules;
- starting global scans and project scans;
- managing repository operations;
- viewing findings, jobs, module states and audit log;
- viewing configuration.

Frontend technology stack:

- `Web UI`: `React`, `TypeScript`, `Vite`, `TanStack Router`, `TanStack Query`, `Zod`, `React Hook Form`, `Ant Design`;
- local `GUI`: `Tauri` over the same React/TypeScript codebase.

Stage 08 includes full MVP administrative screens for backend APIs that are in
the target MVP release scope. Platform-only administrative hardening and
capabilities outside the MVP backend scope are delivered separately:

- full SCIM sync management is deferred to Stage 13;
- advanced tool profile administration and platform-only controls can be
  hardened in Stage 12;
- bundled policy pack management depends on Stage 11 scope.

`GUI` works only locally. Remote access is provided through `Web UI`.

Only one `t-helper` runtime is active in a single local installation:

- if `thelper` is already running for the `Web UI`, the Tauri GUI connects to the existing runtime;
- if `thelper` is not running yet, the Tauri GUI starts a local `thelper`, and the `Web UI` then connects to the same runtime;
- a repeated `thelper` start must not create a second active runtime.

## Non-Functional Requirements

### Performance

- `global-scanner` minimizes the number of `stat/open` operations;
- `global-scanner` uses a worker pool with a concurrency limit; SQLite runtime
  may use effective traversal concurrency 1 to avoid local writer contention;
- `security-validator` uses a separate task queue;
- background jobs are executed by separate worker processes;
- global and project scanning run independently.

### Scalability

- multiple global scan paths are supported;
- modules are logically isolated;
- `global-scanner`, `project-scanner`, `security-validator`, `repository-manager`, `auth` can be moved to separate processes or nodes.

### Reliability

- errors in individual directories do not interrupt the entire scan job;
- errors in individual project checks do not interrupt the global scan and repository operations;
- repeated scan, clone, pull and sync operations must bring the system to the same target state when inputs are unchanged;
- repeated operations must not create duplicate `projects`, `repositories`, `jobs` or active `job_locks`;
- administrative and background operations must be logged.

### Security

- the frontend requires authentication;
- local auth must support login, logout, current session, password reset and password change through the documented backend API;
- access to operations is controlled through `RBAC`;
- database and Git secrets are not stored in plaintext;
- code and findings are not sent outside;
- security rules and policies are used only locally;
- an audit log must be maintained.

## MVP Constraints

- A Terraform project is identified only by the presence of `*.tf`;
- the basic ignore matcher in the MVP may be exclude-only; `!pattern` support is allowed as a separate extension;
- `terragrunt.hcl` support is not mandatory for the first version;
- runtime verification through `terraform init` and `terraform validate` may be moved into a separate worker;
- `GUI` is not a remote client.
