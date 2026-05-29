# ADR 0013: Repository identity

## Status

Accepted.

## Decision

Repository identity is defined by the tuple:

```text
provider + provider_host + full_path
```

`repositories.full_path` is not globally unique by itself.

The `repositories` entity must include:

- `provider` - repository provider type, for example `gitlab`, `github`, `bitbucket` or `generic`;
- `provider_host` - normalized provider host or self-hosted instance host;
- `full_path` - normalized namespace/project path inside that provider instance.

Storage uniqueness uses:

```text
repositories.provider + repositories.provider_host + repositories.full_path
```

Normalization rules:

- `provider_host` is lower-case host name from clone URL, provider configuration or explicit clone request;
- `provider_host` includes a non-default port when it is required to distinguish provider instances;
- `provider_host` does not include protocol, username, token, path, query or fragment;
- `full_path` does not include protocol, host, query, fragment, leading slash or trailing `.git`;
- `full_path` uses `/` as separator;
- provider adapters follow provider-specific parsing rules from ADR 0016 and must produce the same canonical identity fields for equivalent URLs.

`clone_url` is optional transport metadata, not the repository identity key. It may differ for `ssh` and `https` and may change without changing repository identity.

`clone_url` rules:

- `clone_url` is nullable;
- `clone_url` is not unique;
- `clone_url` must not be used for repository lookup, upsert or deduplication;
- persisted `clone_url` must be a safe normalized transport URL without credentials, tokens, passwords or other userinfo;
- if an input URL contains credentials, provider adapters must strip credentials before persistence and use secret references or provider credential configuration for runtime authentication;
- different transport URLs that normalize to the same `provider + provider_host + full_path` must resolve to the same repository card.

## Rationale

Enterprise and on-premise installations can use multiple repository providers and multiple self-hosted instances of the same provider. The same namespace/project path can validly exist in GitLab, GitHub, Bitbucket or in separate GitLab instances.

Using global `full_path` uniqueness would prevent valid repositories from being registered together. Using `clone_url` as identity would couple repository identity to transport protocol and credentials handling.

## Consequences

- Stage 05 migrations must add `provider_host` and enforce uniqueness on `provider`, `provider_host` and `full_path`.
- Repository API DTOs and job payloads must carry `provider_host`.
- Clone/import idempotency must resolve repository identity through `provider`, `provider_host` and `full_path`.
- `clone_url` must remain nullable and non-unique.
- `clone_url` must not be the primary deduplication rule.
- Persisted `clone_url` must not contain credentials, secret material or URL userinfo.
- UI lists and search results should display provider and provider host when showing `full_path`.
- Provider adapters must expose deterministic URL parsing and identity normalization tests.
