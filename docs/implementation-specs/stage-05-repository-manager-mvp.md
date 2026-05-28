# Stage 05: Repository Manager MVP

## Цель

Добавить управляемые repository operations поверх registry, не нарушая инварианты путей, идемпотентности и сериализации конкурентных действий.

## Implementation status

Stage 05 is implemented as the current repository manager MVP baseline. The
remaining repository work is split into later stages:

- Stage 05A: additional managed providers and recursive GitLab group clone;
- Stage 05B: polling-based sync and scheduler integration;
- Stage 14: webhook-based sync.

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

## Entry baseline

Stage 05 starts from a completed Stage 04 scanner/registry baseline:

- `repositories` already exists with `provider`, `provider_host`, `full_path`,
  lifecycle fields and uniqueness indexes for provider-aware and generic local
  identities;
- Stage 04 `project_discovery` can create generic local repository cards and
  `project_links`;
- `GET /api/repos` and `GET /api/repos/{id}` are read-only Stage 04 routes;
- the job system already accepts `repo_clone`, `repo_pull` and `repo_sync`
  payload schemas and maps them to `repository_operation` workflow status;
- worker runtime and `job_locks` exist;
- the module registry contains `repository-manager`.

Closed Stage 05 gaps in the implementation:

- `POST /api/repos/clone`, `POST /api/repos/pull` and `POST /api/repos/sync`
  are executable routes;
- `repository_provider_instances`, `repository_credentials` and
  `repository_operation_reservations` exist for provider profiles, credentials
  and clone pre-create identity/path reservations;
- provider URL parsing and path normalization are implemented as
  repository-manager domain services;
- repository operation conflict lookup is cross-operation for `repo_clone`,
  `repo_pull` and `repo_sync`, with clone pre-create identity/path checks.

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
- provider adapter для одного managed provider: `github`;
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
- selected credentials are validated for provider instance ownership, required usage and transport protocol compatibility;
- Git operations run non-interactively and repository operation failure messages are redacted before being persisted on repository cards;
- released/expired clone pre-create reservations are pruned after the retention window;
- provider URL parsing follows ADR 0016 and equivalent HTTPS/SSH/scp-like URLs resolve to the same repository identity;
- `local_path` всегда внутри выбранного `root_path`;
- path traversal отклоняется на API и domain layer;
- новый root path при clone сохраняется в `root_paths`;
- конфликтующие repo operations сериализуются через `job_locks` and the MVP conflict policy below;
- provider-aware operations reject `status = superseded` repositories with controlled validation error.
- Stage 05 MVP `repo_sync` is pull-only: the worker runs the same Git pull
  path as `repo_pull` and records `operation = repo_sync`; project/security
  scan orchestration is intentionally outside this stage.

## Implementation sequencing

Stage 05 should be implemented in this order to keep the risk surface small:

1. Add repository-manager storage migrations and stores for
   `repository_provider_instances` and `repository_credentials`, including
   `secretref://env/...` validation and masked API responses.
2. Add provider URL parsing and repository identity normalization tests before
   wiring clone/pull/sync. The MVP managed provider must be selected explicitly
   in the implementation branch: `gitlab` or `github`.
3. Add clone target normalization and containment checks as a domain service
   used by both API handlers and worker handlers.
4. Add repository operation enqueue APIs with idempotency and conflict handling.
5. Add worker handlers for `repo_clone`, `repo_pull` and `repo_sync`, then enable
   the `repository-manager` module.
6. Add filesystem/Git integration tests after pure normalization, credential and
   conflict tests are passing.

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
- this check is cross-operation: an active `repo_clone` blocks `repo_pull` and
  `repo_sync` for the same repository, and the same applies to every other
  pair among `repo_clone`, `repo_pull` and `repo_sync`;
- implementation must not rely only on a helper that searches by exact
  `job_type + lock_key`; repository operation conflict lookup must search all
  active repository operation job types for the same `lock_key`;
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
6. Worker execution acquires the final repository lock
   `repository:<repository_id>`.
7. Before writing to disk, the worker also holds the normalized target path lock
   `repository-path:<root_path_id>:<normalized_target_path>` or an equivalent
   repository-manager storage lock, then re-validates the target path against
   the persisted `root_path` and expected repository identity.

The identity lock prevents duplicate clone jobs for the same Git repository
before a stable repository row exists. The path lock prevents two different
repositories from writing into the same local directory. The final
`repository:<repository_id>` lock remains the common serialization key for later
`pull` and `sync`.

Pre-create and worker-side identity/path locks may be represented as queued job
lock keys, multiple held `job_locks`, transactional conflict rows or an
equivalent repository-manager storage primitive. They must expire or be released
safely when enqueue or execution fails so a failed clone request cannot
permanently block later operations.

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

The implementation must include domain-level tests for the same path safety
rules in addition to HTTP API tests. Worker handlers must repeat containment
checks immediately before filesystem writes, because queued jobs may run after
root paths or symlinks have changed.

## Acceptance checklist

Stage 05 is considered complete only when the implementation and tests cover the
following checklist:

- provider profile APIs validate `api_base_url` and `web_base_url` as safe
  HTTPS URLs without userinfo and with a host matching normalized
  `provider_host`;
- managed provider `clone_url` is used for identity extraction and mismatch
  validation, while the selected `protocol` controls the persisted transport
  URL;
- HTTP clone requests reject credentials embedded in URLs and exact
  `Idempotency-Key` replay returns the existing `job_ref`;
- active `repo_clone`, `repo_pull` and `repo_sync` conflict checks are
  cross-operation for the same `repository:<id>` lock key;
- clone pre-create locking rejects duplicate normalized repository identities
  and duplicate normalized target paths before filesystem side effects;
- worker execution repeats target containment checks and rejects symlink escapes
  that appear after enqueue;
- existing empty target directories are accepted after containment/conflict
  checks, existing non-empty non-Git directories are rejected, matching existing
  Git remotes degrade clone to pull, and mismatched Git remotes are rejected;
- provider-aware enrichment keeps generic repository IDs when no canonical
  provider card exists, and relinks projects/project links plus supersedes the
  generic row when a canonical card already exists;
- repository operation job payloads contain `credential_id` only and never raw
  `secretref` values or resolved secrets;
- repository operation failure messages are redacted before persistence.

## Acceptance matrix

| Requirement | Implementation | Test coverage |
| --- | --- | --- |
| Provider profile APIs validate safe HTTPS `api_base_url`/`web_base_url` without userinfo and with matching `provider_host`. | `internal/repository/store.go` provider instance normalization; `internal/httpapi/repository.go` provider instance routes. | `TestProviderInstanceValidatesProfileURLs`, `TestStage05ProviderProfileAPIValidatesSafeHTTPSURLs`. |
| Managed provider `clone_url` is used for identity extraction and mismatch validation; selected `protocol` controls persisted transport URL. | `internal/repository/domain.go` `NormalizeIdentity`, `ParseCloneURL`, `TransportURL`. | `TestNormalizeIdentityGitHubEquivalentURLs`, `TestNormalizeIdentityUsesSelectedProtocolForManagedTransport`, `TestNormalizeIdentityRejectsExplicitURLMismatch`. |
| HTTP clone rejects credentials embedded in URLs and exact `Idempotency-Key` replay returns the existing `job_ref`. | `internal/httpapi/repository.go` clone normalization and `cloneIdempotentReplay`. | `TestStage05RepositoryCloneValidationCodeAndIdempotentReplay`. |
| Active `repo_clone`, `repo_pull` and `repo_sync` conflict checks are cross-operation for `repository:<id>`. | `internal/jobs/store.go` `ActiveRepositoryOperation`; repository operation enqueue paths. | `TestStage05PullAndSyncConflictAcrossRepositoryOperationTypes`, `TestStage05CloneConflictWithActivePullDoesNotMutateRepository`. |
| Clone pre-create locking rejects duplicate normalized repository identities and duplicate normalized target paths before filesystem side effects. | `repository_operation_reservations`, `IdentityReservationKey`, `TargetReservationKey`, clone API reservation flow. | `TestStage05RepositoryCloneValidationCodeAndIdempotentReplay`, `TestStage05CloneRejectsBusyTargetPathForDifferentRepository`, `TestStage05CloneConflictDoesNotCreateNewRootPath`. |
| Worker repeats target containment checks and rejects symlink escapes that appear after enqueue. | `internal/repository/handlers.go` `handleClone`; `internal/repository/domain.go` `NormalizeTarget`. | `TestOperationHandlerCloneRejectsSymlinkEscapeChangedAfterEnqueue`, `TestNormalizeTargetRejectsSymlinkEscapeInExistingParent`. |
| Existing empty target directories are accepted, non-empty non-Git directories are rejected, matching Git remotes degrade clone to pull, and mismatched remotes are rejected. | `internal/repository/handlers.go` clone worker filesystem and remote checks. | `TestOperationHandlerCloneUsesExistingEmptyTargetDirectory`, `TestOperationHandlerCloneNonEmptyTargetRejectsAndRecordsLastError`, `TestOperationHandlerCloneExistingExpectedRemoteRunsPull`, `TestOperationHandlerCloneExistingDifferentRemoteRejects`. |
| Provider-aware enrichment keeps generic repository IDs or relinks/supersedes generic rows when a canonical card exists, without merging project rows. | `internal/repository/store.go` `UpsertRepositoryForClone`, `supersedeGenericRepository`. | `TestUpsertRepositoryEnrichesGenericRepositoryInPlace`, `TestUpsertRepositoryRelinksAndSupersedesGenericRepository`. |
| Repository operation job payloads contain `credential_id` only and never raw `secretref` values or resolved secrets. | `internal/httpapi/repository.go` repo operation payload construction; worker resolves secrets only at execution time. | `TestStage05RepositoryOperationPayloadsDoNotCarrySecretRefs`. |
| Repository operation failure messages are redacted before persistence. | `internal/repository/handlers.go` `recordRepositoryFailure`, `redactRepositoryMessage`. | `TestGitCommandEnvIsNonInteractiveAndRepositoryMessagesAreRedacted`, `TestOperationHandlerCloneNonEmptyTargetRejectsAndRecordsLastError`. |
| Clone target selection is unambiguous and path traversal is rejected at API/domain boundaries. | `internal/httpapi/repository.go` `cloneTargetDirectory`; `internal/repository/domain.go` `NormalizeTarget`, `NormalizeFullPath`. | `TestStage05CloneRejectsAmbiguousTargetDirectoryFields`, `TestNormalizeTargetRejectsTraversal`, `TestNormalizeFullPathRejectsUnsafeSegmentsBeforeCleaning`. |
| Superseded repositories reject provider-aware operations with controlled validation errors. | `internal/httpapi/repository.go` existing operation enqueue validation; `internal/repository/handlers.go` worker validation. | `TestStage05PullRejectsSupersededRepository`. |

## Stage-local blockers

- none for starting implementation.
- MVP managed provider selection: `github`.

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
