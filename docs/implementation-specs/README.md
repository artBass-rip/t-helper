# Пакет ТЗ по stage внедрения

Этот каталог содержит primary implementation specs для актуального roadmap из `docs/roadmap.md`.

Каждый `stage-*` файл является самостоятельным delivery package с единым набором секций:

- `Inputs`
- `Deliverables`
- `Definition of Done`
- секции решений по taxonomy из Stage 00: `Remaining MVP blockers`, `Stage-local blockers`, `Platform blockers` или `Deferred / platform decisions`
- `Traceability`
- `Non-goals`

Implementation-level решения, обязательные для этих ТЗ, зафиксированы в `docs/adr/`.

Stage 00 является каноническим владельцем классов решений. `Remaining MVP
blockers: none` означает, что Stage 01 и MVP scaffolding sequence могут
начинаться без дополнительных архитектурных решений; это не означает, что все
stage-local или platform decisions для последующих этапов уже закрыты.

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

Общий аналитический обзор ведётся через Stage 00 decision register, roadmap и traceability.

Accepted Stage 00 entry/exit summary, Stage 01 scaffolding checklist and
Stage 01-03 backlog are consolidated in
`docs/stage-00-delivery-contract.md`.

Repository identity для Stage 05 и последующих этапов зафиксирован в `docs/adr/0013-repository-identity.md`: `provider + provider_host + full_path`.

Repository provider integration UX для Stage 05/08 зафиксирован в `docs/adr/0015-repository-provider-integration-profiles.md`: multi-host provider profiles и multi-credential per host.

Repository provider URL parsing для Stage 05 зафиксирован в `docs/adr/0016-repository-provider-url-parsing.md`.

Security finding fingerprint для Stage 06 зафиксирован в `docs/adr/0017-security-finding-fingerprint.md`.

External toolchain compatibility поставляется отдельным Stage 06A и зафиксирована в `docs/adr/0018-external-toolchain-profiles.md`: versioned tool profiles, certified compatibility, profile validation and optional profile analyzer. Stage 06B project/security scanning depends on Stage 06A and must not parse external CLI output directly.
