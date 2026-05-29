# ADR 0012: Provider adapter modularity

## Status

Accepted.

## Decision

All external providers must be implemented as separate pluggable modules or libraries behind stable internal interfaces.

This rule applies at minimum to:

- database/storage providers: `sqlite`, `postgresql`, `mysql`, `mssql`;
- managed database engine flavors: Aurora PostgreSQL through `postgresql`, Aurora MySQL through `mysql`;
- authentication providers: local auth and future external IAM/SSO providers;
- SCIM providers;
- repository providers: `gitlab`, `github`, `bitbucket`, `azure_devops`;
- policy/tool providers where runtime integration is provider-specific.

Provider-specific code must not be embedded directly in HTTP handlers, CLI commands or domain logic. Provider modules/libraries are selected through configuration and registered in provider registries.

Database providers are implemented as storage adapter libraries under the storage layer. Auth providers are implemented as auth provider libraries under the auth module. Repository providers are implemented as repository-manager provider libraries.

Provider modules/libraries must expose capability metadata such as provider name, supported engine flavor or protocol, health/check support and whether reload/restart is required after configuration changes.

Repository provider adapters must follow repository identity normalization from ADR 0013: `provider + provider_host + full_path`. URL parsing follows ADR 0016.

## Rationale

Provider-specific behavior changes independently from core domain contracts. Keeping providers behind small interfaces prevents storage, auth and repository integrations from leaking into API handlers, CLI commands, migrations orchestration or business services.

This is also required for on-premise delivery, where different installations may enable different database, auth and repository integrations.

## Consequences

- Stage 01 storage adapters must be pluggable libraries selected by storage configuration.
- Stage 07 auth providers must be pluggable libraries selected by auth configuration.
- Unknown provider names are validation errors.
- Provider capability metadata must be visible enough for diagnostics and controlled errors.
- Tests must include provider registry validation and at least one fake/provider test double where practical.
- Repository provider tests must cover identity normalization across providers and self-hosted provider instances.
