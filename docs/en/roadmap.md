# Roadmap and Acceptance

## Optimized Implementation Stages

The decomposition reduces the size of the foundation portion and makes each
stage independently verifiable. Detailed implementation specs are in
`docs/en/implementation-specs/`.

### Stage 00. Delivery contract

- fix the Definition of Done for the MVP and platform release;
- assign open product decisions the status `accepted`, `deferred` or `out-of-scope` in the Stage 00 decision register;
- fix the future code repository structure, style guides, CI/scaffolding checklist and traceability update rules as a documentation contract;
- normalize reference examples for configuration and secrets.

Executable scaffolding, actual CI files, Go modules and storage test harnesses
are Stage 01 implementation deliverables. Stage 00 is considered complete when
the delivery decisions and documentation contracts are accepted.

### Stage 01. Backend storage foundation

Status: completed.

- created the Go module and executable entrypoints `thelper`, `thelper-worker`, `thelper-ctl`;
- implemented the HTTP adapter skeleton on `net/http`/`chi`;
- implemented storage abstraction, provider registry and migration framework;
- supported `SQLite` and `PostgreSQL` for Stage 01-owned system tables;
- added baseline logging, correlation IDs and `GET /api/health`;
- added SQLite/PostgreSQL storage contract tests and GitHub Actions CI gate.

### Stage 02. Config, modules and runtime lifecycle

Status: completed.

- implemented `config_entries`, `storage_profiles`,
  `storage_provider_settings`, `module_states` and imported system
  `ignore_rules`;
- implemented import through `thelper-ctl -reconfigure`;
- implemented the reloadability matrix and synchronous `thelper-ctl -reload`
  without a Stage 03 `jobs` dependency;
- implemented module registry with lifecycle `start`, `stop`, `reload`, `health`;
- implemented `thelper-ctl -restart <module>` and Stage 02 sync result DTOs;
- fixed the singleton runtime lock/health mechanism for local mode.

### Stage 03. Jobs, workers and status foundation

Status: completed.

- implemented `jobs`, `job_locks`, `job_events`, `workflow_statuses`;
- implemented atomic job claim, leases, heartbeat, expired lease recovery and retry/backoff;
- moved background job execution into `thelper-worker`;
- implemented worker handlers for `config_reload` and `module_restart`;
- started the baseline `status-monitor` for jobs/workers/modules.

### Stage 04. Scanner and registry MVP

Status: completed.

- implemented `root_paths`, `ignore_rules`, `projects`, `environments`, `workspaces` and a minimal `repositories` registry;
- implemented an exclude-only ignore matcher with preservation of `!pattern`;
- implemented Terraform project discovery by `*.tf` without reading source contents;
- implemented coalesced background `project_discovery` jobs for determining project Git relationships without blocking the global scan;
- implemented Git marker discovery inside `project_discovery` and stopping traversal below a discovered Terraform project in global scan;
- implemented `project_links` for relating separate project records that belong to one Git repository without merging project rows;
- closed the API for scan roots, ignore rules, scans, projects, environments and workspaces.

### Stage 05. Repository manager MVP

Status: completed.

- implemented the expanded `repositories` model;
- implemented `clone`, `pull`, `sync` through jobs and `job_locks`;
- ensured path safety: normalization, path traversal rejection, `local_path` only inside the selected `root_path`;
- fixed repository identity as `provider + provider_host + full_path`;
- implemented GitKraken-like provider integration profiles: multi-host and multi-credential per host;
- implemented MVP adapters for `generic` Git and GitHub;
- left `bitbucket`, `azure_devops`, recursive GitLab group clone, webhook sync and polling sync outside the Stage 05 MVP.

### Stage 05A. Repository operations extensions

- add the second managed provider from the `gitlab`/`github` pair if it was not included in Stage 05;
- implement recursive clone of GitLab group/subgroups after single-repository clone stabilizes;
- add provider adapters for `bitbucket` and `azure_devops`;
- preserve the same repository identity, credential and path safety contract as Stage 05.

### Stage 05B. Repository polling sync

- implement polling-based sync as a separate repository workflow;
- add scheduler integration for polling;
- verify that polling does not bypass `job_locks`, credential usage validation or status-monitor aggregation.

### Stage 06. Project scanner and security validator MVP

- implement `project_scan_settings`, `project_security_scan_settings`, `project_scans`;
- implement parent-child workflow `project_scan` -> `security_validation_scan` through `job_group_id`;
- connect `terraform validate` and `TFLint` for `project-scanner`;
- connect `Trivy` as the mandatory security scanner for the MVP;
- leave `Gitleaks`, `Checkov`, `OPA` and `Conftest` as adapter extension points without mandatory MVP acceptance;
- store `security_rule_sets`, `security_findings` and aggregate status through `status-monitor`.

Stage 06 intentionally split into two delivery packages:

- Stage 06A: external toolchain profile runtime, registry, validation and certified bundled profiles for `terraform validate`, `TFLint` and `Trivy`;
- Stage 06B: project scanner and security validator orchestration using only the Stage 06A profile runtime.

### Stage 07. Auth, RBAC, SCIM and audit

- implement local authentication and the external auth providers framework;
- implement local credentials storage with Argon2id PHC password hashes according to ADR 0014;
- implement the first-run bootstrap admin user flow;
- implement users, groups, memberships, roles, permissions and role bindings;
- enable API authorization enforcement according to the matrix in `access-control.md`;
- implement SCIM contract/stub without a full sync workflow in the MVP;
- write audit security events.

### Stage 08. Frontend MVP and local GUI

- implement a unified React/TypeScript frontend for `Web UI` and `GUI`;
- use Vite, TanStack Router, TanStack Query, Zod, React Hook Form and Ant Design;
- cover the main read/operate scenarios: projects, roots, ignore rules, scans, repos, findings, jobs, modules, audit, config;
- implement full administrative screens for auth/RBAC/configuration/security rule sets because the corresponding backend APIs are in the target MVP release scope;
- implement the Tauri GUI only for local mode;
- verify that `GUI` and `Web UI` use only the documented backend API;
- provide feature parity for MVP read/operate and MVP administrative scenarios;

Stage 08 entry contract for route map, navigation, operational density and
Tauri local runtime discovery/start policy is accepted in
[`frontend-ui-contract.md`](frontend-ui-contract.md). Packaging/signing and
update/distribution policy remain release artifact exit decisions.

### Stage 09. Runtime and observability hardening

- expand observability and `status-monitor`;
- strengthen scheduler, worker shutdown/recovery and module runtime;
- complete the prioritized runtime/scanner optimization backlog from the Stage 09 implementation spec and [`code-optimization.md`](code-optimization.md);
- implement full `.gitignore` semantics with `!pattern`;
- implement optional `follow_symlinks = true` hardening with cycle detection, root containment checks and traversal guards if this mode is approved;
- formalize degraded states, retention cleanup and operator diagnostics for jobs, locks, modules, workers and scans.

### Stage 10. Storage adapter expansion

- add `MySQL`, `MSSQL` and other approved SQL-compatible adapters;
- deliver dialect-specific migrations with synchronized logical migration versions;
- expand the shared storage contract suite to all target adapters;
- document dialect-specific behavior differences and application-level validation fallback.

### Stage 11. Security tooling and policy packs

- define and deliver baseline local rule sets and enterprise-policy packs;
- connect additional security adapters: `Gitleaks`, `Checkov`, `OPA` and `Conftest`;
- use ADR 0018 tool profiles for additional tools;
- verify locality of the security stack and stability of ADR 0017 findings fingerprints.

### Stage 12. Admin UI hardening

- hardened extensions for administrative UI beyond the Stage 08 full MVP admin screens;
- add UI for advanced tool profile administration, SCIM sync management and platform-only administrative workflows where in release scope;
- add SCIM visibility and sync operation surfaces where backend APIs are available;
- preserve `Web UI`/`GUI` parity according to `docs/en/frontend-ui-contract.md`.

### Stage 13. SCIM full sync

- implement a full SCIM sync workflow over the Stage 07 contract/stub;
- fix conflict policy, mapping rules, audit events and partial failure behavior;
- connect scheduled/manual sync through jobs, worker leases and status-monitor.

### Stage 14. Repository webhook sync

- implement webhook-based repository sync;
- add provider-specific webhook payload validation and secret verification;
- enqueue repository sync jobs without bypassing `job_locks`;
- provide audit/status integration for webhook events.

### Stage 15. Distributed deployment

- move `global-scanner`, `project-scanner`, `repository-manager`, `security-validator`, `auth` and `status-monitor` into compatible runtime modes;
- formalize inter-module retry, timeout, idempotency and auth contracts;
- prepare service discovery, health model, worker groups and HA topology;
- verify that distributed mode does not introduce a second source of truth and does not break API/storage contracts.

## Acceptance Criteria

### MVP acceptance

| ID | Criterion |
| --- | --- |
| `ACC-MVP-001` | A Terraform project is discovered by the presence of `*.tf`. |
| `ACC-MVP-002` | Global scanning identifies Terraform projects, and background `project_discovery` identifies project Git relationships. |
| `ACC-MVP-003` | After a project is discovered, nested directories are not scanned as separate working directories. |
| `ACC-MVP-004` | Ignore rules correctly exclude files and directories; `!pattern` is preserved without data loss and applied after full `.gitignore` semantics are implemented. |
| `ACC-MVP-005` | Projects are stored as separate records, and project Git relationships are stored without merging project records. |
| `ACC-MVP-006` | Project-level scan identifies providers, required auth, syntax issues, deprecations and quality issues through `terraform validate` and `TFLint`, while security/validation scan stores findings through `Trivy` as the mandatory local scanner. |
| `ACC-MVP-007` | Runtime configuration is stored in the database. |
| `ACC-MVP-008` | `thelper-ctl -reconfigure` imports configuration and ignore rules into the database. |
| `ACC-MVP-009` | `thelper-ctl -reload` applies reloadable configuration. |
| `ACC-MVP-010` | `thelper-ctl -restart <module>` works for any individual module. |
| `ACC-MVP-011` | `GUI` and `Web UI` use a unified backend API and cover MVP read/operate scenarios. |
| `ACC-MVP-012` | `GUI` works only locally. |
| `ACC-MVP-013` | `PostgreSQL` and `SQLite` are supported through storage abstraction. |
| `ACC-MVP-014` | `clone`, `pull`, `sync` do not lead to inconsistent state and are serialized through `job_locks`. |
| `ACC-MVP-015` | `environments` and `workspaces` are supported. |
| `ACC-MVP-016` | `auth` is implemented as a separate module. |
| `ACC-MVP-017` | Local auth and `RBAC` are implemented at backend/API level; SCIM endpoints may be contract/stub without a full sync workflow. |
| `ACC-MVP-018` | The security stack works locally and does not send code outside. |
| `ACC-MVP-019` | Security findings and rule sets are stored inside the system. |
| `ACC-MVP-020` | One project scan API creates `project_scans` and a parent-child jobs workflow without a separate `security_scans` entity; security findings are read through scoped or security endpoints. |
| `ACC-MVP-021` | Backend API covers scan roots, repositories, jobs, environments, workspaces and module states for the Frontend MVP. |
| `ACC-MVP-022` | Clone workflow supports `generic` Git and one managed provider from `gitlab` or `github`, `https|ssh` selection, root path and target directory selection, multi-host/multi-credential provider profiles, path safety and `job_locks`. |

### Platform acceptance

| ID | Criterion |
| --- | --- |
| `ACC-PLATFORM-001` | All target SQL/SQL-like databases on the roadmap are supported: `PostgreSQL`, `SQLite`, `MySQL`, `MSSQL` and additional SQL-compatible adapters if approved for the platform release. |
| `ACC-PLATFORM-002` | Full administrative UI is implemented for auth, RBAC and `SCIM`. |
| `ACC-PLATFORM-003` | UI for editing configuration and security rule sets is implemented. |
| `ACC-PLATFORM-004` | Full `.gitignore` semantics with negative `!pattern` rules are implemented. |
| `ACC-PLATFORM-005` | Distributed deployment is prepared for `global-scanner`, `project-scanner`, `security-validator`, `repository-manager`, `auth`, `web`, `nginx` and the database. |
| `ACC-PLATFORM-006` | Repository integrations are expanded to `gitlab`, `github`, `bitbucket`, `azure_devops`, recursive GitLab group clone, polling sync and webhook sync. |
| `ACC-PLATFORM-007` | Security adapters are expanded to `Trivy`, `Gitleaks`, `Checkov`, `OPA` and `Conftest` with baseline local rule sets/policy packs. |
| `ACC-PLATFORM-008` | A full SCIM sync workflow is implemented over the Stage 07 SCIM contract/stub. |

## Open Decision Status

The canonical decision status is recorded in
`docs/en/implementation-specs/stage-00-delivery-contract.md` in the `Decision
register` section. If the roadmap and Stage 00 diverge, the Stage 00 decision
register is used as the more precise delivery contract for implementation
management.

- MVP breadth: accepted, the MVP remains a broad platform slice through Stage 08 and is governed by stage ownership/test gates;
- bootstrap admin recovery model: accepted, strict first-run recovery policy remains without an unauthenticated persistent recovery path;
- `terragrunt.hcl` support: deferred, not included in the MVP;
- baseline local security rules and policy packs: deferred until Stage 11 unless approved earlier;
- minimal external auth providers: deferred, Stage 07 delivers local auth and provider interface;
- `project` / `environment` / `workspace` lifecycle: accepted for MVP in conservative mode;
- full `.gitignore` semantics: deferred until Stage 09, MVP matcher is exclude-only with preservation of `!pattern`.
