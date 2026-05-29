# ADR 0015: Repository provider integration profiles and credentials

## Status

Accepted.

## Decision

Repository integrations use a GitKraken-like profile model with explicit multi-host and multi-credential support.

The model has two separate concepts:

- repository provider instance/profile - a configured provider host, for example GitHub Cloud, GitHub Enterprise Server, GitLab Cloud, GitLab Self-Managed, Bitbucket Cloud, Bitbucket Data Center or Azure DevOps organization/server;
- repository credential - one auth material reference with explicit usages/capabilities, for example git transport, provider API or webhook verification.

Repository provider instances are first-class records. A single provider can have multiple configured hosts:

```text
gitlab + gitlab.com
gitlab + gitlab.foodtech.team
gitlab + gitlab.company-a.internal
github + github.com
github + ghe.company.internal
bitbucket + bitbucket.org
bitbucket + bitbucket.company.internal
azure_devops + dev.azure.com/company
```

Repository credentials are also first-class records. One provider instance may have multiple credentials with different permissions, scopes and operational purpose.

Credential usages:

```text
git_transport
provider_api
webhook
```

Credential auth types:

```text
ssh_key
https_token
https_basic
oauth_token
app_password
webhook_secret
```

All secret values are stored as secret references using ADR 0009. MVP supports `secretref://env/...`. Raw tokens, passwords, private keys, passphrases and webhook secrets must not be accepted for persistence.

Repository operations pass `credential_id`, not raw secret references, in job payloads. Workers load the credential record and resolve secret refs at use time.

Provider API operations such as GitLab recursive group clone require a credential with `provider_api` usage. Git transport operations require a credential with `git_transport` usage. Webhook verification requires a credential with `webhook` usage. A credential may support multiple usages only when the underlying auth material and provider semantics allow it.

## Rationale

On-premise and enterprise environments commonly use multiple hosts for the same Git provider. Different tokens on the same host can have different permissions: read-only clone, group listing, webhook management or admin-level repository operations.

Keeping provider instances separate from credentials avoids coupling host identity, account/profile UX and secret material. It also lets UI expose an integration experience similar to GitKraken while preserving the on-premise secret model.

## Consequences

- Stage 05 adds `repository_provider_instances` and `repository_credentials`.
- `repositories` references a provider instance and may reference a default credential.
- Repository operation APIs accept `provider_instance_id` and/or normalized `provider + provider_host`; credential selection is explicit through `credential_id`.
- `jobs.repo_*` payloads carry `credential_id` only.
- API responses mask secret refs and never return resolved secrets.
- UI must support multiple provider hosts and multiple credentials per host.
- Tests must cover multi-host identity, multi-credential selection, usage validation and secret masking.
- Provider URL parsing follows ADR 0016.
