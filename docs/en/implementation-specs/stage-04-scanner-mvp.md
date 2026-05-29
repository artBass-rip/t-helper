# Stage 04: Scanner and Registry MVP

## Goal

Implement a discovery layer that finds Terraform projects under `root_path`, applies ignore rules, registers each locally discovered project as a separate DB record and enqueues background project discovery jobs for later Git relationship detection.

## Inputs

- `docs/en/requirements.md`
- `docs/en/interfaces.md`
- `docs/en/api.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/test-plan.md`

## Scope

- `global-scanner` module;
- `root_paths`, `ignore_rules`, `projects`;
- minimal registry-mode `repositories` through background project discovery jobs, not directly from global filesystem traversal;
- `project_links` for linking separate local project records that belong to one Git repository;
- `environments` and `workspaces` backend read models/API for the Frontend MVP;
- conservative MVP lifecycle for `project` / `environment` / `workspace`: scanner creates and updates `projects`, while links to `environment` and `workspace` are preserved only if already known or explicitly set;
- bounded worker pool for traversal; SQLite runtime uses an effective traversal
  concurrency of 1 to avoid local database writer contention, while other
  storage providers use bounded parallel traversal;
- exclude-only matcher with preservation of `!pattern`;
- Terraform project detection by `*.tf`;
- enqueue a background `project_discovery` job for each created or updated project;
- determine project Git relationships only in the `project_discovery` job using the MVP Git marker allowlist: `.git/` directory or `.git` file whose first non-empty line is `gitdir:`;
- negative handling for `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` inside `project_discovery`;
- API for scan/root paths/ignore rules/projects/environments/workspaces.

## Non-goals

- provider-aware repository operations;
- provider-aware URL parsing, remote enrichment and clone/pull/sync behavior from Stage 05;
- reading `.git/config` or shelling out to `git` from `global-scanner`;
- project-level checks and security findings;
- auth enforcement beyond documented permission contracts;
- automatic linking rules for `project` / `environment` / `workspace` based on naming conventions or Terraform source parsing;
- full `.gitignore` semantics.
- `follow_symlinks = true`; MVP supports only `follow_symlinks = false`, extended symlink traversal is deferred to Stage 09 runtime hardening.

## Deliverables

- `global-scanner` module;
- `project_discovery` worker handler for local project metadata discovery;
- migrations/read models for `root_paths`, `ignore_rules`, `projects`, `project_links`, `environments`, `workspaces`;
- registry API for root paths, scans, projects, environments, workspaces and ignore rules;
- scan fixtures and acceptance tests;
- discovery algorithm notes.

## Implementation contract

### Discovery algorithm notes

- `POST /api/scans` and `jobs.global_scan.payload.v1` resolve requested
  `root_path_ids` through the registry and deduplicate repeated IDs while
  preserving request order.
- `global-scanner` materializes `scanning.global_scan`, loads ignore rules per
  root, then traverses each root with a bounded in-process worker pool. SQLite
  runs traversal with effective concurrency `1`; other providers use
  `workers.concurrency` capped at `64`, with default `4`.
- Traversal uses directory entries and `lstat` metadata only. Terraform project
  detection checks entry names for `*.tf`; it does not open Terraform source
  files and stops traversal below a detected Terraform working directory.
- `.git` directories are skipped by global traversal. Repository detection is
  deferred to `project_discovery`, which walks upward from the project path to
  the root path and checks only the allowlisted `.git` marker.
- Stage 04 filesystem boundary tests use an injected filesystem adapter to
  assert that global scan does not open Terraform source, `.git` marker files or
  `.git/config`, and that project discovery reads at most `4 KiB` from `.git`
  marker files.

### Upsert and identity rules

- `root_paths` upsert key: normalized absolute `path`.
- Public `PUT /api/root-paths` treats `root_paths.source` as read-only.
  API-created or API-updated rows are owned as `source = api`; `source =
  config` is reserved for materialized `scanning.global_scan` entries.
- `projects` upsert key: `root_path_id + relative_path`.
- Global filesystem scan never merges local project records. Every discovered Terraform working directory gets its own `projects` row keyed by `root_path_id + relative_path`.
- Project records are created with the full project schema from the beginning; fields whose information is not known yet remain nullable/default and are filled later by project discovery, repository manager, project scans or explicit user/admin input.
- `global_scan` fills `projects.name`, `projects.path`, `projects.relative_path`, `projects.root_path_id`, `projects.terraform_marker`, `projects.status = active`, `projects.detected_at`, `projects.last_seen_at`, `projects.created_at` and `projects.updated_at`.
- `project_discovery` fills `projects.repository_id` when repository membership is known and creates `project_links` for same-repository relationships.
- Stage 05 repository manager enriches repository cards, not project rows; project rows remain separate and only their `repository_id` relation may be updated.
- Stage 06 project/security scans write scan data to scan/finding tables, not summary fields on `projects`.
- Manual/admin input may set `projects.environment_id`, `projects.default_workspace_id` and future explicit metadata fields.
- `repositories` provider-aware upsert key remains `provider + provider_host + full_path`.
- Stage 04 generic local fallback identity is scoped by root path:
  `provider + provider_host + root_path_id + full_path`. This prevents two
  different scan roots with the same relative repository path from being merged.
- Generic local repository upsert must be idempotent under concurrent
  `project_discovery` jobs for projects in the same Git repository. A unique
  identity conflict is handled by re-reading/updating the existing repository
  card rather than failing the job.
- Stage 04 global scan does not create repository cards directly. It enqueues `jobs.job_type = project_discovery` for each created/updated project and continues traversal without waiting for that job.
- `project_discovery` performs local Git marker discovery for one project. If the project belongs to a Git working tree but provider identity is not known, project discovery creates or updates a conservative generic repository card:
  - `provider = generic`;
  - `provider_host = local`;
  - `full_path = <root_path-relative normalized repository path>`;
  - `root_path_id` points to the containing root path;
  - `local_path` points to the normalized discovered repository path;
  - `clone_url = null`.
- If multiple local project records belong to the same Git repository, project discovery does not merge project records. It sets the same `repository_id` where known and writes `project_links` with `link_type = same_repository`.
- Stage 05 provider-aware repository manager may later enrich repository cards after explicit provider URL parsing/remote validation, but it must not merge separate project records into one project.

### Filesystem read boundaries

- `global-scanner` reads directory entries and metadata required for traversal.
- `global-scanner` must not open `*.tf` files during Stage 04 discovery; Terraform project detection is based on filenames only.
- `global-scanner` must not read `.git`, `.git/config` or execute `git`.
- `project_discovery` may read a `.git` regular file only as Git metadata and only up to an implementation limit of `4 KiB`; marker is valid when the first non-empty line starts with `gitdir:`.
- `project_discovery` must not execute `git` or Terraform/toolchain commands.

### Symlink policy

- Default Stage 04 behavior is `follow_symlinks = false`.
- Symlinked directories are skipped by default and counted as skipped directories.
- A root path that is itself a symlink is rejected for Stage 04 traversal when
  `follow_symlinks = false`; the job fails if no requested root path can be
  processed.
- `follow_symlinks = true` is not supported in MVP and is deferred to Stage 09. A future implementation must include opt-in configuration, cycle detection, root containment checks, traversal counters and max-depth/max-visited guards.

### Ignore-rule behavior

- Ignore rules are applied relative to `root_path`.
- MVP matcher is exclude-only.
- Rule order is preserved on import/storage for future full `.gitignore` semantics.
- Negative rules beginning with `!` are imported and returned by API without loss, but do not affect Stage 04 matching.
- Ignored directories are not traversed deeper.

### Scan result and status policy

- `projects_created` counts newly inserted project rows.
- `projects_updated` counts existing project rows whose discovery metadata or `last_seen_at` is updated.
- `project_discovery_jobs_enqueued` counts background project discovery jobs created by global scan.
- Repository counters are not part of `jobs.global_scan.result.v1`; repository cards are created/updated by `project_discovery` jobs.
- `directories_skipped` counts ignored directories, skipped symlink directories and immediate child directories skipped because traversal stops below a detected Terraform working directory.
- Per-directory errors are recorded in `job_events` and increment `errors_count`.
- If at least one requested root path is processed successfully, the job may finish with `jobs.status = succeeded` even when `errors_count > 0`.
- If all requested root paths fail before useful traversal, the job must finish with `jobs.status = failed`.
- `jobs.status` does not use `partial`; partial scan quality is represented through `errors_count`, result payload counters and `job_events`.
- Missing previously discovered projects are not deleted in MVP. If a project under a scanned `root_path` is not seen in a completed scan, scanner sets `projects.status = missing` and keeps the row for audit/UI continuity.
- Missing status does not replace or merge project records. If the project is later discovered again, the same row returns to `active`.
- `GET /api/projects` returns `active` projects by default. Missing/disabled projects are returned only when `status?` explicitly requests them or when `status=all`.
- Missing projects are read-only for project scan operations until rediscovered; `POST /api/project-scans` must reject `projects.status = missing` with a controlled validation error.
- Stale `project_discovery` jobs for `projects.status = missing` or
  `disabled` are no-op and must not create repository cards, links or
  `projects.repository_id`.
- If a later global scan finds the same `root_path_id + relative_path`, the project is automatically reactivated by setting `status = active` and updating `last_seen_at`.

### Required fixtures

- `basic_tf_project`: directory with `main.tf`.
- `nested_tf_project`: parent with `main.tf` and nested `child/main.tf`; nested child is not registered separately.
- `git_repo_marker_directory`: valid `.git/` directory for `project_discovery`.
- `git_repo_marker_file`: valid `.git` regular file with first non-empty line starting with `gitdir:` for `project_discovery`.
- `git_repo_marker_file_invalid`: `.git` regular file without valid `gitdir:`.
- `same_git_repository_projects`: two Terraform working directories related to the same Git repository; separate project rows remain and `project_links.link_type = same_repository` is created.
- negative marker fixtures: `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep`.
- `ignored_directory`: directory excluded by `.t-helper.ignore`.
- `negative_ignore_pattern`: `!pattern` stored without applying in exclude-only matcher.

## Definition of Done

- scan detects Terraform working directories by `*.tf`;
- global scan creates/updates only project records and enqueues `project_discovery` jobs;
- `project_discovery` detects only allowed Git markers: `.git/` directory and `.git` file with `gitdir:`;
- similar files/directories `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` do not create a repository link/card;
- scanner does not shell out to `git` during discovery;
- scanner does not read Terraform source and does not read `.git/config` in Stage 04;
- filesystem-only Git repositories without provider identity are registered by the project discovery job as `provider = generic`, `provider_host = local`;
- different Terraform projects inside one Git repository remain separate `projects` rows and are linked through `project_links`;
- symlinked directories skipped by default when `follow_symlinks = false`;
- nested Terraform directories under a found project are not registered separately;
- repeated scan does not create duplicates;
- concurrent `project_discovery` jobs for one Git repository do not create
  duplicate generic repository cards and do not fail on repository identity
  conflicts;
- projects found earlier but absent from a new scan are not deleted and receive `status = missing`;
- `!pattern` is preserved without being applied in the exclude-only matcher;
- scanner does not create an implicit `environment` or `workspace` without an explicit input rule/data;
- if a project's link to `environment` or `workspace` is already known, repeated scan preserves it;
- environments/workspaces read endpoints work and have FK/read tests;
- `root_paths.source` ownership is enforced: API cannot claim config-owned
  source values through request payloads;
- stale project discovery for missing/disabled projects does not mutate
  repository registry state;
- results are written to `jobs.global_scan.result.v1`.

## Remaining MVP blockers

- no Stage 04 blockers remain for the Git marker allowlist; the MVP allowlist is closed by this spec.

## Deferred decisions

- automatic linking rules for `project` / `environment` / `workspace`;
- default seed/import behavior for environments/workspaces beyond minimal read models and FK constraints.

## Traceability

- Roadmap: Stage 04.
- Acceptance: `ACC-MVP-001`, `ACC-MVP-002`, `ACC-MVP-003`, `ACC-MVP-004`, `ACC-MVP-005`, `ACC-MVP-015`, `ACC-MVP-021`.
- API: root paths, scans, projects, environments, workspaces, ignore rules.
- Data model: `root_paths`, `ignore_rules`, `projects`, `project_links`, `repositories`, `environments`, `workspaces`.

## Risks

- incorrect path normalization and duplicate project cards;
- excessive number of `stat/open` operations in a large tree;
- incorrect handling of `.git` file for worktree/submodule.
