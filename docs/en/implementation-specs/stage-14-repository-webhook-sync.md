# Stage 14: Repository Webhook Sync

## Goal

Add webhook-based repository sync without bypassing Stage 05 repository identity, credential usage, job locking and status-monitor contracts.

## Inputs

- `docs/en/architecture.md`
- `docs/en/api.md`
- `docs/en/data-model.md`
- `docs/en/payload-schemas.md`
- `docs/en/access-control.md`
- `docs/en/test-plan.md`
- `docs/en/adr/0015-repository-provider-integration-profiles.md`
- `docs/en/adr/0016-repository-provider-url-parsing.md`

## Scope

- webhook endpoints and provider payload validation;
- webhook secret verification through repository credentials with `webhook` usage;
- provider-specific webhook adapters for supported providers;
- enqueue repository sync jobs;
- audit/status integration;
- webhook retry/failure diagnostics.

## Non-goals

- polling sync, already covered by repository polling extension scope;
- recursive clone workflows;
- distributed webhook ingress unless included later in Stage 15 deployment scope.

## Deliverables

- webhook receiver API;
- provider webhook payload adapters;
- webhook secret verification;
- repository sync job enqueueing;
- audit events and status visibility;
- webhook fixtures and negative tests.

## Definition of Done

- webhook sync never bypasses `job_locks`;
- provider payloads are validated before enqueueing sync jobs;
- `webhook` credential usage is enforced;
- invalid signatures and unsupported payloads return controlled errors;
- sync jobs are observable through jobs/status endpoints;
- webhook events are audited without storing secrets.

## Platform blockers

- supported provider set for webhook sync in the target platform release;
- webhook ingress security policy.

## Traceability

- Roadmap: Stage 14.
- Acceptance: `ACC-PLATFORM-006`.
- API: repository webhook endpoints, repository sync jobs.
- Data model: `repositories`, `repository_credentials`, `jobs`, `job_locks`, `audit_log`.
- ADR: `0015`, `0016`.

## Risks

- webhook handlers bypass repository locks;
- provider-specific payload parsing leaks into domain services;
- webhook secrets appear in logs/events.
