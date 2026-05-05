# Stage 05: Repository Manager MVP

## Цель

Добавить управляемые repository operations поверх registry, не нарушая инварианты путей, идемпотентности и сериализации конкурентных действий.

## Inputs

- `docs/requirements.md`
- `docs/architecture.md`
- `docs/api.md`
- `docs/data-model.md`
- `docs/payload-schemas.md`
- `docs/test-plan.md`
- `docs/adr/0013-repository-identity.md`
- `docs/adr/0015-repository-provider-integration-profiles.md`
- `docs/adr/0016-repository-provider-url-parsing.md`

## Scope

- модуль `repository-manager`;
- полноценная модель `repositories`;
- provider-aware clone adapters для `generic` Git и одного managed provider: `gitlab` или `github`;
- repository identity `provider + provider_host + full_path`;
- repository card enrichment for Stage 04 generic filesystem-discovered repositories;
- GitKraken-like provider integration profiles with multi-host support;
- multi-credential support per provider host with usage validation;
- `clone`, `pull`, `sync`;
- `job_locks` для сериализации;
- API и jobs repository operations.

## Non-goals

- project/security scan orchestration после clone/pull;
- UI clone workflow;
- расширенные auth providers;
- distributed execution repository-manager.
- webhook-based sync;
- polling-based sync;
- recursive GitLab group/subgroups clone;
- `bitbucket` и `azure_devops` adapters.

## Deliverables

- repository-manager module;
- provider adapter для `generic` Git;
- provider adapter для одного managed provider: `gitlab` или `github`;
- provider instance/profile API and storage;
- repository credentials API and storage using `secretref://...`;
- normalized repository identity handling with `provider_host`;
- repository lifecycle fields: `status`, `discovery_source`, `superseded_by_repository_id`, `identity_confirmed_at`;
- repo operation jobs и payload handlers;
- safe path normalization;
- repository API;
- concurrency tests.

## Definition of Done

- clone/pull/sync создают jobs и выполняются через worker;
- repository cards are unique by `provider + provider_host + full_path`;
- repository manager enriches repository cards and relinks project relations when needed, but never merges separate `projects` rows;
- adapters normalize `provider_host` and `full_path` before repository lookup or creation;
- `clone_url` is nullable, non-unique transport metadata and is not used for lookup/upsert/deduplication;
- persisted `clone_url` is safe-normalized and contains no credentials, tokens, passwords or userinfo;
- different SSH/HTTPS transport URLs for the same `provider + provider_host + full_path` resolve to the same repository card;
- one provider can have multiple configured hosts/provider instances;
- one provider instance can have multiple credentials with different usages/permissions;
- repository operation jobs carry `credential_id`, not raw secret refs or resolved secrets;
- selected credentials are validated for provider instance ownership and required usage;
- provider URL parsing follows ADR 0016 and equivalent HTTPS/SSH/scp-like URLs resolve to the same repository identity;
- `local_path` всегда внутри выбранного `root_path`;
- path traversal отклоняется на API и domain layer;
- новый root path при clone сохраняется в `root_paths`;
- конфликтующие repo operations сериализуются через `job_locks` and the MVP conflict policy below;
- provider-aware operations reject `status = superseded` repositories with controlled validation error.

## Repository enrichment contract

Stage 04 `project_discovery` may create conservative local cards:

- `provider = generic`
- `provider_host = local`
- `full_path = <root_path-relative normalized repository path>`
- `status = active`
- `discovery_source = filesystem`
- `identity_confirmed_at = null`

When Stage 05 resolves provider-aware identity for the same local repository, repository manager applies one of two flows.

### Enrich in place

If a matching provider-aware card does not already exist:

- update the existing generic repository row in place;
- set `provider`, `provider_host`, `full_path`, safe `clone_url`, `default_branch` and provider metadata from resolved identity;
- set `discovery_source = provider` or `clone`, depending on the operation source;
- set `identity_confirmed_at = now`;
- keep the same `repositories.id`;
- keep existing `projects.repository_id` relations and `project_links`.

### Relink and supersede

If a matching provider-aware card already exists:

- keep the provider-aware card as the canonical repository row;
- update projects pointing to the generic row so they point to the provider-aware row;
- preserve separate project records and existing `project_links`, updating `project_links.repository_id` where applicable;
- set the generic row to `status = superseded`;
- set `superseded_by_repository_id` to the canonical provider-aware repository id;
- do not hard delete the generic row in MVP.

In both flows, project rows remain separate. A repository identity match never collapses multiple Terraform projects into one project record.

## Repository operation conflict policy

MVP policy for concurrent `clone`, `pull` and `sync` operations is intentionally
strict and predictable:

- at most one active repository operation job may exist for one
  `lock_key = repository:<repository_id>`;
- `clone` must also use pre-create conflict checks before `repository_id` is
  available;
- active means `jobs.status in (queued, running)`;
- exact replay with the same `Idempotency-Key` and same payload returns the
  existing `job_ref`;
- replay with the same `Idempotency-Key` and different payload returns a
  controlled conflict or validation error;
- a new request without the same `Idempotency-Key` returns `409 conflict` when
  another active repository operation exists for the same repository;
- conflict errors use machine-readable code
  `repository_operation_already_running`;
- conflict error details include `repository_id`, `lock_key`,
  `active_job_id` and `active_job_type`;
- MVP does not merge, replace, cancel, supersede or auto-chain conflicting
  repository operation jobs;
- clone may degrade to pull only when the target already contains the expected
  Git repository and no active repository operation exists for that repository.

### Clone pre-create locking

`POST /api/repos/clone` must serialize clone creation before filesystem side
effects and before relying on an existing `repository_id`.

Required API/domain flow:

1. Normalize request input into canonical `provider`, `provider_host`,
   `full_path`, `protocol`, selected `root_path` and normalized target path.
2. Check active jobs/locks by normalized repository identity using
   `repository-identity:<provider>:<provider_host>:<full_path>`.
3. Check active jobs/locks by normalized local target using
   `repository-path:<root_path_id>:<normalized_target_path>`.
4. Upsert or find `repositories` by `provider + provider_host + full_path`.
5. Create the job with final `lock_key = repository:<repository_id>`.
6. Worker execution acquires `job_locks` for `repository:<repository_id>` and
   the normalized target path lock before writing to disk.

The identity lock prevents duplicate clone jobs for the same Git repository
before a stable repository row exists. The path lock prevents two different
repositories from writing into the same local directory. The final
`repository:<repository_id>` lock remains the common serialization key for later
`pull` and `sync`.

Conflict errors from pre-create locking use machine-readable codes:

- `repository_operation_already_running` for identity conflicts;
- `repository_target_path_busy` for target path conflicts.

## Local path safety examples

Repository manager must normalize and validate every clone target before creating
jobs or filesystem side effects.

Required cases:

- `../` path traversal in `target_directory`, `new_target_directory`, repository
  name or provider path must be rejected before path creation;
- a symlink inside `root_path` must not allow `local_path` to escape the selected
  root after symlink evaluation;
- case-insensitive filesystems must not allow duplicate logical targets that differ
  only by case when the host filesystem treats them as the same path;
- Unicode paths must be normalized consistently before uniqueness checks and root
  containment checks;
- existing empty directory may be used as clone target only after containment and
  conflict checks;
- existing non-empty directory without expected Git metadata must return a
  controlled validation error;
- existing Git repository with the expected remote may turn clone into pull;
- existing Git repository with a different remote must reject clone unless an
  explicit future takeover workflow is documented.

## Stage-local blockers

- none.

## Traceability

- Roadmap: Stage 05.
- Acceptance: `ACC-MVP-014`, `ACC-MVP-021`, `ACC-MVP-022`.
- API: `GET /api/repos`, `GET/PUT /api/repo-provider-instances`, `GET/PUT /api/repo-credentials`, `POST /api/repos/clone|pull|sync`.
- Data model: `repositories`, `repository_provider_instances`, `repository_credentials`, `jobs`, `job_locks`, `root_paths`.
- ADR: `0013`, `0015`, `0016`.

## Риски

- ошибки path normalization приведут к записи за пределы root;
- provider adapters разойдутся по payload/transport behavior;
- расширение provider set после MVP может нарушить URL parsing contract, если adapters не используют ADR 0016.
