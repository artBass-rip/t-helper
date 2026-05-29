# Stage 11: Security Tooling and Policy Packs

## Goal

Expand the local security stack beyond the Stage 06B MVP scanner while preserving local-only execution, ADR 0018 tool profiles and ADR 0017 finding fingerprint semantics.

## Inputs

- `docs/en/roadmap.md`
- `docs/en/test-plan.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/api.md`
- `docs/en/adr/0017-security-finding-fingerprint.md`
- `docs/en/adr/0018-external-toolchain-profiles.md`

## Scope

- `Gitleaks`;
- `Checkov` as an additional scanner after the mandatory Stage 06B `Trivy`;
- `OPA`;
- `Conftest`;
- baseline local rule sets;
- enterprise policy packs;
- profile files, validation fixtures and normalized DTO mapping for new tools;
- rule set import/activation lifecycle.

## Non-goals

- external SaaS findings transfer;
- administrative UI for policy authoring;
- distributed scanner execution.

## Deliverables

- additional security adapters through ADR 0018 profiles;
- baseline local rule sets;
- enterprise policy pack packaging/import contract;
- expanded finding normalization/fingerprint tests;
- local-only security acceptance suite.

## Definition of Done

- all added tools execute locally and do not make outbound SaaS calls for findings, source code or rule evaluation;
- all added tool outputs normalize through approved tool profiles;
- rule sets and policy packs are stored locally and versioned;
- findings use stable ADR 0017 fingerprints across repeated scans;
- bundled rule sets and policy packs are documented and local-only.

## Platform blockers

- baseline security rules and bundled policy packs;
- policy pack format for OPA/Conftest.

## Traceability

- Roadmap: Stage 11.
- Acceptance: `ACC-PLATFORM-007`.
- Data model: `security_rule_sets`, `security_findings`, `tool_profiles`, `tool_profile_validation_results`.
- ADR: `0017`, `0018`.

## Risks

- scanner output drift changes normalized findings;
- policy packs grow without ownership/versioning;
- tools accidentally perform network operations.
