# Stage 04: Scanner and Registry MVP

## Цель

Реализовать discovery-слой, который обнаруживает Terraform-проекты в `root_path`, применяет ignore rules, регистрирует каждый локально обнаруженный проект отдельной записью в БД и ставит фоновые project discovery jobs для последующего определения Git-связей.

## Inputs

- `docs/requirements.md`
- `docs/interfaces.md`
- `docs/api.md`
- `docs/data-model.md`
- `docs/payload-schemas.md`
- `docs/test-plan.md`

## Scope

- модуль `global-scanner`;
- `root_paths`, `ignore_rules`, `projects`;
- минимальный registry-mode `repositories` через фоновые project discovery jobs, а не напрямую из global filesystem traversal;
- `project_links` для связи отдельных локальных project records, относящихся к одному Git repository;
- `environments` и `workspaces` backend read models/API для Frontend MVP;
- conservative MVP lifecycle для `project` / `environment` / `workspace`: scanner создаёт и обновляет `projects`, а связи с `environment` и `workspace` сохраняются только если они уже известны или заданы явно;
- bounded worker pool обхода; SQLite runtime uses an effective traversal
  concurrency of 1 to avoid local database writer contention, while other
  storage providers use bounded parallel traversal;
- exclude-only matcher с сохранением `!pattern`;
- обнаружение Terraform-проектов по `*.tf`;
- enqueue фонового `project_discovery` job для каждого созданного или обновлённого проекта;
- определение Git-связей проекта только в `project_discovery` job по MVP Git marker allowlist: `.git/` directory или `.git` file с первой непустой строкой `gitdir:`;
- negative handling для `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` внутри `project_discovery`;
- API для scan/root paths/ignore rules/projects/environments/workspaces.

## Non-goals

- provider-aware repository operations;
- provider-aware URL parsing, remote enrichment and clone/pull/sync behavior from Stage 05;
- чтение `.git/config` или shell-out в `git` из `global-scanner`;
- project-level checks и security findings;
- auth enforcement beyond documented permission contracts;
- automatic linking rules для `project` / `environment` / `workspace` на основе naming conventions или Terraform source parsing;
- full `.gitignore` semantics.
- `follow_symlinks = true`; MVP supports only `follow_symlinks = false`, extended symlink traversal is deferred to Stage 09 runtime hardening.

## Deliverables

- модуль `global-scanner`;
- `project_discovery` worker handler for local project metadata discovery;
- migrations/read models для `root_paths`, `ignore_rules`, `projects`, `project_links`, `environments`, `workspaces`;
- registry API для root paths, scans, projects, environments, workspaces и ignore rules;
- scan fixtures и acceptance tests;
- discovery algorithm notes.

## Implementation contract

### Upsert and identity rules

- `root_paths` upsert key: normalized absolute `path`.
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

- scan обнаруживает Terraform working directory по `*.tf`;
- global scan creates/updates only project records and enqueues `project_discovery` jobs;
- `project_discovery` обнаруживает только разрешённые Git markers: `.git/` directory и `.git` file с `gitdir:`;
- похожие файлы/директории `.gitignore`, `.gitattributes`, `.gitmodules`, `.gitlab-ci.yml`, `.github/`, `.gitkeep` не создают repository link/card;
- scanner не выполняет shell-out в `git` во время discovery;
- scanner не читает Terraform source и не читает `.git/config` на Stage 04;
- filesystem-only Git repositories без provider identity регистрируются project discovery job'ом как `provider = generic`, `provider_host = local`;
- разные Terraform projects внутри одного Git repository остаются отдельными `projects` rows and are linked through `project_links`;
- symlinked directories skipped by default when `follow_symlinks = false`;
- nested Terraform directories под найденным project не регистрируются отдельно;
- повторный scan не создаёт дубли;
- concurrent `project_discovery` jobs for one Git repository do not create
  duplicate generic repository cards and do not fail on repository identity
  conflicts;
- ранее найденные, но отсутствующие в новом scan projects не удаляются и получают `status = missing`;
- `!pattern` сохраняется без применения в exclude-only matcher;
- scanner не создаёт implicit `environment` или `workspace` без явного входного правила/данных;
- если связь проекта с `environment` или `workspace` уже известна, повторный scan сохраняет её;
- environments/workspaces read endpoints работают и имеют FK/read tests;
- results пишутся в `jobs.global_scan.result.v1`.

## Remaining MVP blockers

- нет Stage 04 blockers по Git marker allowlist; MVP allowlist закрыт этим spec.

## Deferred decisions

- automatic linking rules для `project` / `environment` / `workspace`;
- default seed/import behavior для environments/workspaces сверх минимальных read models и FK constraints.

## Traceability

- Roadmap: Stage 04.
- Acceptance: `ACC-MVP-001`, `ACC-MVP-002`, `ACC-MVP-003`, `ACC-MVP-004`, `ACC-MVP-005`, `ACC-MVP-015`, `ACC-MVP-021`.
- API: root paths, scans, projects, environments, workspaces, ignore rules.
- Data model: `root_paths`, `ignore_rules`, `projects`, `project_links`, `repositories`, `environments`, `workspaces`.

## Риски

- ошибочная нормализация путей и дубли карточек проектов;
- чрезмерное число `stat/open` операций при большом дереве;
- неправильная обработка `.git` file для worktree/submodule.
