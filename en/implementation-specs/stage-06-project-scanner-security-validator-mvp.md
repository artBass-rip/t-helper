# Stage 06B: Project Scanner and Security Validator MVP

## Цель

Реализовать локальный анализ Terraform-проектов и security/validation checks как orchestration layer над локальными CLI-инструментами, используя только Stage 06A tool profile runtime.

## Inputs

- `docs/requirements.md`
- `docs/architecture.md`
- `docs/api.md`
- `docs/data-model.md`
- `docs/payload-schemas.md`
- `docs/test-plan.md`
- `docs/adr/0006-project-scan-workflow-and-status-aggregation.md`
- `docs/adr/0017-security-finding-fingerprint.md`
- `docs/adr/0018-external-toolchain-profiles.md`
- `docs/implementation-specs/stage-06a-toolchain-profiles-foundation.md`

## Scope

- модуль `project-scanner`;
- модуль `security-validator`;
- `project_scan_settings`;
- `project_security_scan_settings`;
- `project_scans`;
- `security_rule_sets`;
- `security_findings`;
- parent-child workflow `project_scan` -> `security_validation_scan`;
- `terraform validate`, `TFLint`;
- `Trivy` как обязательный MVP security scanner;
- integration with Stage 06A profile-based compatibility layer for external CLI tools;
- adapter extension points для `Gitleaks`, `Checkov`, `OPA` и `Conftest`;
- scoped и global findings API.

## Non-goals

- administrative UI для rule sets;
- external findings transfer;
- отдельная сущность/API `security_scans`;
- distributed scanner execution.
- обязательная MVP-приёмка для всех security tools сразу;
- bundled policy packs для OPA/Conftest.

## Deliverables

- project scanner module;
- security validator module;
- scan settings API;
- finding storage и rule set registry;
- normalized scan DTO mapping from tool profiles;
- local toolchain test harness;
- aggregate status integration.

## Definition of Done

- `POST /api/project-scans` создаёт `project_scans` и parent job;
- child `security_validation_scan` jobs используют тот же `job_group_id`;
- parent job не ждёт child jobs;
- `status-monitor` обновляет aggregate `project_scans.status/result_payload`;
- security modules запускаются только если включены в project settings;
- обязательная MVP-приёмка требует успешной интеграции `Trivy`;
- external tool output is parsed only through approved tool profiles from ADR 0018;
- certified tool versions and profiles from Stage 06A are used for Stage 06B acceptance tests;
- unsupported, missing or uncertified tools return controlled machine-readable errors according to configured version policy;
- findings use ADR 0017 fingerprints and repeated scans update existing finding rows by fingerprint;
- `security_findings.fingerprint` is unique and `first_seen_at`/`last_seen_at` are maintained;
- payload/result не содержит raw Terraform source или secrets;
- network-restricted scan не делает outbound calls.

## Stage-local blockers

- Stage 06A must be complete before Stage 06B starts.

## Deferred / platform decisions

- baseline bundled rule sets;
- policy pack format для OPA/Conftest.

## Traceability

- Roadmap: Stage 06.
- Acceptance: `ACC-MVP-006`, `ACC-MVP-018`, `ACC-MVP-019`, `ACC-MVP-020`.
- API: project scans, scan settings, findings, rule sets.
- Data model: `project_scans`, scan settings, `security_findings`, `security_rule_sets`, `tool_profiles`, jobs.
- ADR: `0006`, `0017`, `0018`.

## Риски

- смешение summary results и detailed findings;
- нестабильность external CLI output formats;
- автоматическая генерация профилей без явной валидации может изменить fingerprint semantics;
- рост времени выполнения без отдельной очереди security jobs.
