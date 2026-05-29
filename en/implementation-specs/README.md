# Implementation Stage Specification Package

This directory contains the primary implementation specs for the current roadmap in `docs/en/roadmap.md`.

Each `stage-*` file is a standalone delivery package with a shared section set:

- `Inputs`
- `Deliverables`
- `Definition of Done`
- decision sections using the Stage 00 taxonomy: `Remaining MVP blockers`, `Stage-local blockers`, `Platform blockers` or `Deferred / platform decisions`
- `Traceability`
- `Non-goals`

Implementation-level decisions required by these specs are recorded in `docs/en/adr/`.

Stage 00 is the canonical owner of decision classes. `Remaining MVP blockers:
none` means that Stage 01 and the MVP scaffolding sequence can start without
additional architectural decisions; it does not mean that all stage-local or
platform decisions for later stages are already closed.

## Stage index

- `stage-00-delivery-contract.md`
- `stage-01-backend-storage-foundation.md`
- `stage-02-config-modules-runtime.md`
- `stage-03-jobs-workers-status.md`
- `stage-04-scanner-mvp.md`
- `stage-05-repository-manager-mvp.md`
- `stage-06a-toolchain-profiles-foundation.md`
- `stage-06-project-scanner-security-validator-mvp.md`
- `stage-07-auth-scim-rbac.md`
- `stage-08-frontend-mvp.md`
- `stage-09-runtime-observability-hardening.md`
- `stage-10-storage-adapter-expansion.md`
- `stage-11-security-tooling-policy-packs.md`
- `stage-12-admin-ui-hardening.md`
- `stage-13-scim-full-sync.md`
- `stage-14-repository-webhook-sync.md`
- `stage-15-distributed-deployment.md`

The overall analytical overview is maintained through the Stage 00 decision register, roadmap and traceability.

Accepted Stage 00 entry/exit summary, Stage 01 scaffolding checklist and
Stage 01-03 backlog are consolidated in
`docs/en/stage-00-delivery-contract.md`.

Repository identity for Stage 05 and later stages is fixed in `docs/en/adr/0013-repository-identity.md`: `provider + provider_host + full_path`.

Repository provider integration UX for Stage 05/08 is fixed in `docs/en/adr/0015-repository-provider-integration-profiles.md`: multi-host provider profiles and multi-credential per host.

Repository provider URL parsing for Stage 05 is fixed in `docs/en/adr/0016-repository-provider-url-parsing.md`.

Security finding fingerprint for Stage 06 is fixed in `docs/en/adr/0017-security-finding-fingerprint.md`.

External toolchain compatibility is delivered by a separate Stage 06A and fixed in `docs/en/adr/0018-external-toolchain-profiles.md`: versioned tool profiles, certified compatibility, profile validation and optional profile analyzer. Stage 06B project/security scanning depends on Stage 06A and must not parse external CLI output directly.
