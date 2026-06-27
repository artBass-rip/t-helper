# Stage 06B: Project Scanner and Security Validator MVP

## Goal

Implement local Terraform project analysis and security/validation checks as an orchestration layer over local CLI tools, using only the Stage 06A tool profile runtime.

## Inputs

- `docs/en/requirements.md`
- `docs/en/architecture.md`
- `docs/en/api.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/test-plan.md`
- `docs/en/adr/0006-project-scan-workflow-and-status-aggregation.md`
- `docs/en/adr/0017-security-finding-fingerprint.md`
- `docs/en/adr/0018-external-toolchain-profiles.md`
- `docs/en/implementation-specs/stage-06a-toolchain-profiles-foundation.md`

## Scope

- `project-scanner` module;
- `security-validator` module;
- `project_scan_settings`;
- `project_security_scan_settings`;
- `project_scans`;
- `security_rule_sets`;
- `security_findings`;
- parent-child workflow `project_scan` -> `security_validation_scan`;
- `terraform validate`, `TFLint`;
- `Trivy` as the mandatory MVP security scanner;
- integration with Stage 06A profile-based compatibility layer for external CLI tools;
- adapter extension points for `Gitleaks`, `Checkov`, `OPA` and `Conftest`;
- scoped and global findings API.

## Non-goals

- administrative UI for rule sets;
- external findings transfer;
- separate `security_scans` entity/API;
- distributed scanner execution.
- mandatory MVP acceptance for all security tools at once;
- bundled policy packs for OPA/Conftest.

## Deliverables

- project scanner module;
- security validator module;
- scan settings API;
- finding storage and rule set registry;
- normalized scan DTO mapping from tool profiles;
- local toolchain test harness;
- aggregate status integration.

## Definition of Done

- `POST /api/project-scans` creates `project_scans` and a parent job;
- child `security_validation_scan` jobs use the same `job_group_id`;
- parent job does not wait for child jobs;
- `status-monitor` updates aggregate `project_scans.status/result_payload`;
- security modules run only if enabled in project settings;
- mandatory MVP acceptance requires successful `Trivy` integration;
- external tool output is parsed only through approved tool profiles from ADR 0018;
- certified tool versions and profiles from Stage 06A are used for Stage 06B acceptance tests;
- unsupported, missing or uncertified tools return controlled machine-readable errors according to configured version policy;
- findings use ADR 0017 fingerprints and repeated scans update existing finding rows by fingerprint;
- `security_findings.fingerprint` is unique and `first_seen_at`/`last_seen_at` are maintained;
- payload/result does not contain raw Terraform source or secrets;
- network-restricted scan does not make outbound calls.
- the bundled Terraform profile supports Terraform `>=1.15.0`;
- t-helper installation automatically downloads and verifies pinned TFLint and Trivy releases.

## Stage-local blockers

- Stage 06A must be complete before Stage 06B starts.

## Deferred / platform decisions

- baseline bundled rule sets;
- policy pack format for OPA/Conftest.

## Traceability

- Roadmap: Stage 06.
- Acceptance: `ACC-MVP-006`, `ACC-MVP-018`, `ACC-MVP-019`, `ACC-MVP-020`.
- API: project scans, scan settings, findings, rule sets.
- Data model: `project_scans`, scan settings, `security_findings`, `security_rule_sets`, `tool_profiles`, jobs.
- ADR: `0006`, `0017`, `0018`.

## Risks

- mixing summary results and detailed findings;
- unstable external CLI output formats;
- automatic profile generation without explicit validation can change fingerprint semantics;
- runtime growth without a separate security jobs queue.
