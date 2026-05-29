# Stage 06A: Toolchain Profiles Foundation

## Цель

Полностью реализовать ADR 0018 external toolchain profile runtime до scanner/security orchestration, чтобы Stage 06B не парсил CLI output напрямую и не зависел от hard-coded форматов Terraform, TFLint или Trivy.

## Inputs

- `docs/ru/adr/0018-external-toolchain-profiles.md`
- `docs/ru/api.md`
- `docs/ru/data-model.md`
- `docs/ru/payload-schemas.md`
- `docs/ru/configuration.md`
- `docs/ru/test-plan.md`

## Scope

- `tool-profile-runtime`;
- tool runner abstraction;
- version discovery;
- profile registry and active profile selection;
- profile validator;
- approved parser primitives from ADR 0018;
- normalized DTO mapping primitives;
- redaction rules;
- tool profile import/activation APIs;
- optional `tool-profile-analyzer`;
- bundled certified profiles for `terraform validate`, `TFLint` and `Trivy`;
- validation fixtures for MVP tools.

## Non-goals

- running project scans;
- writing `security_findings`;
- project/security scan workflow orchestration;
- bundled OPA/Conftest policy packs;
- mandatory adapters for `Gitleaks`, `Checkov`, `OPA` or `Conftest`.

## Deliverables

- tool profile registry and storage integration;
- `tool_profiles` and `tool_profile_validation_results` stage-owned migrations;
- `GET /api/tool-profiles`;
- `POST /api/tool-profiles/validate`;
- `POST /api/tool-profiles/import`;
- `POST /api/tool-profiles/activate`;
- `POST /api/tool-profiles/analyze` if analyzer is included in the release artifact;
- bundled profile files under `tool-profiles/terraform`, `tool-profiles/tflint` and `tool-profiles/trivy`;
- profile validation fixtures;
- redaction and size-limit tests.

## Definition of Done

- external tool output can be parsed only through approved parser primitives;
- profile validation rejects shell fragments, arbitrary scripts, eval behavior, network calls and file reads outside captured tool output;
- active profile selection respects `certified_only`, `compatible_range` and `latest_best_effort`;
- default MVP policy is `certified_only`;
- imported profiles remain inactive until explicit activation;
- generated analyzer profiles are never activated automatically and must pass validation before import/activation;
- fresh tool versions can be adopted by importing and activating new or updated profile files when existing parser primitives are sufficient;
- raw CLI output is not persisted as the primary contract;
- redaction rules prevent raw Terraform source, secrets, private keys, tokens and credential-bearing URLs from entering jobs, events, workflow summaries, findings or logs;
- unsupported, missing or uncertified tools return controlled machine-readable errors from ADR 0018.

## Certified Tool Profile Baseline

Stage 06A uses ADR 0018 `certified_only` policy for acceptance. Exact patch
versions are fixed at implementation time from the binaries available in
CI/runtime images, but the certified profile set is fixed for:

- `terraform validate`;
- `TFLint`;
- `Trivy`.

Initial bundled validation fixtures must cover at least:

- `terraform validate` success;
- `terraform validate` syntax error;
- `terraform validate` missing provider or required-auth related error;
- `TFLint` warning/error with normalized file path and rule id;
- `Trivy config` misconfiguration finding;
- Trivy output containing secret-like values, proving redaction/masking;
- unsupported tool version;
- missing tool binary;
- malformed tool output.

## Remaining MVP Blockers

- none.

## Traceability

- Roadmap: Stage 06A.
- Acceptance: supports `ACC-MVP-006`, `ACC-MVP-018`, `ACC-MVP-019`, `ACC-MVP-020`.
- API: `GET /api/tool-profiles`, `POST /api/tool-profiles/validate`, `POST /api/tool-profiles/import`, `POST /api/tool-profiles/activate`, `POST /api/tool-profiles/analyze`.
- Data model: `tool_profiles`, `tool_profile_validation_results`.
- ADR: `0018`.

## Риски

- overly flexible profile language may become an execution sandbox risk;
- generated profiles may drift fingerprint semantics if accepted without fixture validation;
- insufficient redaction can leak secrets into diagnostics.
