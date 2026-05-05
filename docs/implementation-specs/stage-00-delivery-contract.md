# Stage 00: Delivery Contract

## Цель

Закрыть продуктовые и engineering-развилки до начала code scaffolding, чтобы последующие stages выполнялись по согласованному Definition of Done.

## Inputs

- `docs/requirements.md`
- `docs/architecture.md`
- `docs/roadmap.md`
- `docs/traceability.md`
- `docs/test-plan.md`
- `docs/adr/`
- `config.example.json`

## Scope

- Definition of Done для MVP и platform release;
- финализация открытых решений: `terragrunt.hcl`, baseline policy packs, минимальные external auth providers, lifecycle связей `project` / `environment` / `workspace`;
- правила ведения roadmap, traceability и implementation specs;
- структура будущего code repository, package layout, style guide и CI skeleton;
- проверка, что scaffolding следует принятым ADR без временных обходных решений;
- фиксация политики секретов для `secretref://...`, masking и ошибок resolution.

## Non-goals

- реализация backend/frontend кода;
- миграции БД;
- запуск scanner/repository/security/auth модулей;
- изменение уже принятых ADR без отдельного ADR supersede process.

## Deliverables

- утверждённый Definition of Done;
- список закрытых и оставшихся blocked decisions;
- Stage 00 decision register с accepted/deferred/out-of-scope статусами;
- нормализованная модель идентификаторов `repository_id`;
- ADR по package layout, migration naming, config key compatibility и secret resolution;
- локальный development contract для SQLite/PostgreSQL storage tests;
- начальный backlog для Stage 01-03;
- правила secret reference handling;
- CI/scaffolding checklist.

Stage 00 deliverables are documentation and decision deliverables. Executable
repository scaffolding, CI files, Go module creation and migration/test harness
implementation are owned by Stage 01.

## Definition of Done

- все открытые решения из `docs/roadmap.md` имеют статус: accepted, deferred или explicitly out of scope;
- `projects.repository_id` используется как единое имя связи проекта с репозиторием в storage/API/payload контрактах;
- `scanning.global_scan` зафиксирован как единый внешний config key и мапится во внутренние `root_paths`;
- package layout и migration naming/versioning зафиксированы отдельным ADR;
- secret resolver contract зафиксирован отдельным ADR;
- локальный PostgreSQL dev/test setup описан в `docs/development.md`;
- `docs/traceability.md` покрывает каждый MVP capability;
- `config.example.json` валиден и не содержит literal-секретов;
- implementation specs используют `ACC-MVP-*` и `ACC-PLATFORM-*` identifiers;
- команда может начать Stage 01 без неявных архитектурных решений.

## Decision register

| Decision | Status | Resolution |
| --- | --- | --- |
| Current repository state | Accepted | Проект сейчас documentation-only/documentation-first; реализация ещё не начата, поэтому Stage 01 is the first code scaffolding stage. |
| Stage 00 delivery boundary | Accepted | Stage 00 is complete as a documentation/decision contract. Repository layout, style guide and CI are specified by ADRs/docs; executable scaffolding, actual CI files and test harnesses are Stage 01 implementation deliverables. |
| MVP breadth | Accepted | MVP intentionally remains a broad platform slice through Stage 08: backend, storage, jobs/workers, scanner, repository manager, tool profiles, local scanners, auth/RBAC, audit, Web UI and local GUI. This is an accepted delivery trade-off to avoid temporary contracts between stages; teams must manage it through strict stage ownership and acceptance tests rather than by shrinking the documented MVP. |
| `terragrunt.hcl` support | Deferred | Не входит в MVP; extension point допускается без discovery/scan behavior. |
| Baseline local security rules and policy packs | Deferred | Stage 06 реализует registration/storage; bundled policy content переносится в Stage 11 unless approved earlier. |
| Minimal external auth providers | Deferred | Stage 07 обязан поставить local auth и provider interface; конкретные external providers утверждаются отдельно. |
| SCIM MVP scope | Accepted | Stage 07 поставляет SCIM contract/stub; полноценный SCIM sync workflow переносится в Stage 13/platform. |
| `project` / `environment` / `workspace` lifecycle | Accepted for MVP | Stage 04 поставляет entities/read API/FK; automatic linking rules остаются conservative и требуют явного импорта/редактирования до отдельного решения. |
| Full `.gitignore` semantics | Deferred | MVP matcher exclude-only; `!pattern` импортируется и хранится без применения до Stage 09. |
| `repo_id` vs `repository_id` | Accepted | Используется единое имя `repository_id`. |
| `scanning.global_scan` spelling | Accepted | Используется единый внешний config key `scanning.global_scan`; внутреннее доменное имя - `root_paths`. |
| Package layout and migration naming | Accepted | См. ADR 0007. |
| Dialect-specific SQL migrations | Accepted | Shared logical migration versions with dialect-specific SQL for `sqlite`, `postgres`, `mysql`, `mssql`; Stage 01 implements SQLite/PostgreSQL, Stage 10 adds MySQL/MSSQL. |
| Secret resolver contract | Accepted | См. ADR 0009; MVP resolver - `secretref://env/...`. |
| Local PostgreSQL development environment | Accepted | См. `docs/development.md`. |
| Singleton runtime lock/health | Accepted | См. ADR 0010. |
| Health endpoint exposure | Accepted | Confirmed delivery decision: `GET /api/health` is unauthenticated safe metadata for local runtime discovery; detailed runtime state remains authenticated under `/api/status`. |
| Runtime auth session API | Accepted | Stage 07 includes login, logout, current session, password reset and password change endpoints in addition to administrative auth/RBAC APIs. |
| Bootstrap admin recovery model | Accepted | Stage 07 keeps the strict first-run bootstrap policy from `data-model.md`: unused bootstrap credentials expire after 24 hours, are not recreated automatically and recovery of an unclaimed empty auth state requires documented destructive reset/reinitialization. This operational trade-off is accepted to avoid an unauthenticated persistent recovery path in MVP. |
| Bulk `PUT` semantics | Accepted | Confirmed delivery decision: MVP bulk `PUT` endpoints are non-destructive upserts by stable identity or `id`; omitted records are not deleted. |
| Public delete endpoints | Accepted | Confirmed delivery decision: delete permissions are seeded for future lifecycle APIs, but public `DELETE` endpoints are out of scope for MVP. |
| MVP lifecycle without public DELETE | Accepted | MVP user-facing lifecycle is expressed through explicit state fields such as `enabled`, `active`, `disabled`, `missing` or `superseded`, and through non-destructive `PUT` updates. UI and API must not imply hard deletion; future delete/archive endpoints require an explicit API contract and migration/test update. |
| Project records and Git relationships | Accepted | Global scan creates separate project records only; `project_discovery` determines Git relationships; multiple projects from one Git repository are linked through `project_links` and are not merged. |
| Missing project lifecycle | Accepted | Missing projects are not deleted, are hidden from default project listing, reject project scan operations, and auto-return to `active` when rediscovered. |
| MVP symlink traversal | Accepted | Stage 04 supports only `follow_symlinks = false`; optional `follow_symlinks = true` is deferred to Stage 09 runtime hardening. |
| Repository enrichment | Accepted | Stage 05 enriches generic repository cards in place where possible, or relinks projects and marks generic cards `superseded`; project rows are never merged. |
| Repository manager MVP scope | Accepted | Stage 05 включает `generic` Git и один managed provider из `gitlab`/`github`; `bitbucket`, `azure_devops` и recursive GitLab group clone переносятся в repository extensions, polling sync - в Stage 05B, webhook sync - в Stage 14. |
| Repository operation conflict policy | Accepted | Stage 05 rejects a new `clone`, `pull` or `sync` request with `409 conflict` when another `queued` or `running` repository operation exists for the same `repository:<id>` lock key, except exact `Idempotency-Key` replay, which returns the existing job reference. |
| Clone pre-create conflict policy | Accepted | `POST /api/repos/clone` must check normalized repository identity and normalized target path locks before a stable `repository_id` exists, then create the job with final `lock_key = repository:<repository_id>`. |
| Security validator MVP scope | Accepted | Stage 06 требует `terraform validate`, `TFLint`, findings model и `Trivy` как обязательный MVP local security scanner; `Checkov`, `Gitleaks`, `OPA` и `Conftest` являются adapters outside mandatory MVP acceptance. |
| Stage 06 split | Accepted | Stage 06A delivers the full ADR 0018 toolchain profile runtime, registry, validator, certified profiles and optional analyzer; Stage 06B delivers project/security scanner orchestration on top of Stage 06A. |
| Stage 06 certified tool profiles | Accepted | Stage 06A uses ADR 0018 `certified_only` profiles for `terraform`, `TFLint` and `Trivy`; initial validation fixtures must cover success, failure, unsupported/missing tools, malformed output and secret redaction cases before Stage 06B acceptance. |
| Storage migration profiles | Accepted | Storage config хранится в profile slots `current` и `migration`; active DB switches only through successful `thelper-ctl -migrate-db`, preserving old DB metadata and leaving old DB deletion manual. This must be implemented fully, not as a temporary simplified MVP behavior. |
| Stage-owned migrations | Accepted | `docs/data-model.md` is the target model; physical migrations are introduced strictly by stage, and a table appears only when that stage ships code, API/worker behavior and tests for its invariants. |
| SQLite worker limits | Accepted | Worker settings are provider/profile-specific; SQLite MVP requires one worker process, concurrency `1`, WAL, foreign keys and busy timeout without affecting PostgreSQL settings. |
| Worker identity, backoff and retention defaults | Accepted | См. ADR 0011. |
| Initial module registry contents | Accepted | Seed includes `core`, `worker-runtime`, `config-manager`, `module-runtime`, `status-monitor`, `global-scanner`, `repository-manager`, `project-scanner`, `security-validator`, `auth`, `web`. |
| Provider adapter modularity | Accepted | Database, auth, SCIM, repository and provider-specific integrations are pluggable modules/libraries behind stable internal interfaces. См. ADR 0012. |
| Repository identity | Accepted | Repository identity is `provider + provider_host + full_path`; `clone_url` is transport metadata, not identity. См. ADR 0013. |
| Local password hashing | Accepted | Local auth uses Argon2id PHC hashes in `local_user_credentials`, reset tokens are hash-only, and lockout defaults are fixed. См. ADR 0014. |
| Repository provider integration profiles | Accepted | Git integrations use multi-host provider profiles and multi-credential records with `secretref://...` secret refs. См. ADR 0015. |
| Repository provider URL parsing | Accepted | Provider adapters normalize GitLab/GitHub/Bitbucket/Azure DevOps URLs into canonical identity fields. См. ADR 0016. |
| Security finding fingerprint | Accepted | Findings use `fp:v1:<sha256 canonical_json>` over stable identity components. См. ADR 0017. |
| External toolchain profiles | Accepted | Stage 06A external CLI compatibility uses versioned tool profiles, certified compatibility, explicit activation and optional profile analyzer. См. ADR 0018. |
| Stage 08 admin UI scope | Accepted | Stage 08 includes full MVP administrative screens for auth/RBAC/configuration/security rule sets in both Web UI and local GUI. Stage 12 is hardening/extension scope, not the first full admin UI delivery. |
| Stage 08 UI delivery contract | Accepted | `docs/frontend-ui-contract.md` is the accepted Stage 08 entry contract for route map, navigation model, operational density and local Tauri runtime discovery/start policy. Packaging/signing/update channel decisions remain exit decisions before release artifact publication. |

## Decision classes

Stage 00 owns the delivery decision taxonomy. The statement that no remaining
blocked decisions exist means there are no unresolved decisions that block the
start of Stage 01 or the MVP scaffolding sequence. Later stages may still have
stage-local or platform decisions that must be resolved before that stage starts,
before that stage reaches DoD, or before the platform release is declared.

- `MVP blocker` - must be resolved before Stage 01 starts or before MVP scope can be implemented safely.
- `Stage-local blocker` - does not block MVP start, but must be resolved before the named stage starts or reaches DoD.
- `Platform blocker` - does not block MVP delivery, but must be resolved before the platform stage that owns the decision.
- `Deferred decision` - explicitly outside the current delivery scope; implementation must keep the documented extension point.

## Remaining MVP blockers

- none.

## Stage-local blockers

| Stage | Decision | Required by |
| --- | --- | --- |
| Stage 08 | Tauri packaging/signing and update/distribution channel policy. | Before Stage 08 release artifact publication. |

## Platform blockers

| Stage | Decision | Required by |
| --- | --- | --- |
| Stage 09 | Observability backend/export format. | Before runtime/observability hardening implementation starts. |
| Stage 10 | Approved list of additional SQL-compatible adapters beyond `MySQL` and `MSSQL`. | Before platform storage scope is committed. |
| Stage 11 | Baseline security rules and bundled policy packs. | Before platform security acceptance. |
| Stage 11 | Policy pack format for OPA/Conftest. | Before policy pack implementation starts. |
| Stage 12 | Final administrative UI scope for the target platform release. | Before admin UI implementation starts. |
| Stage 13 | SCIM sync conflict policy and external provider mapping rules. | Before SCIM full sync implementation starts. |
| Stage 14 | Supported provider set and ingress security policy for webhook sync. | Before webhook sync implementation starts. |
| Stage 15 | Service discovery mechanism. | Before distributed deployment design freeze. |
| Stage 15 | Deployment target stack. | Before distributed deployment design freeze. |
| Stage 15 | Inter-module authentication mechanism. | Before distributed deployment design freeze. |
| Stage 15 | Queue and worker group ownership model. | Before distributed deployment design freeze. |

## Traceability

- Roadmap: Stage 00.
- Acceptance: preparation stage, прямых `ACC-MVP-*` нет.
- ADR: все существующие ADR должны быть проверены перед scaffolding; Stage 00 adds ADR `0007`, `0008`, `0009`, `0010`, `0011`, `0012`, `0013`, `0014`, `0015`, `0016`, `0017` and `0018`.

## Риски

- начало реализации конкретного stage до закрытия его stage-local blockers приведёт к пересмотру data model, API или runtime behavior;
- отсутствие DoD сделает stages формально завершёнными, но непригодными для следующего stage.
