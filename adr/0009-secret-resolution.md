# ADR 0009: Secret resolution

## Status

Accepted.

## Decision

Sensitive configuration values must not be persisted or returned as raw secrets.

MVP secret references use this format:

```text
secretref://env/VARIABLE_NAME
```

The first implementation must provide an environment-backed secret resolver. Additional resolvers such as file, Vault or KMS may be added later without changing config storage contracts.

Sensitive keys include:

- `external_databases.username`;
- `external_databases.password`;
- provider tokens and Git HTTPS tokens;
- webhook secrets;
- auth provider client secrets;
- private keys or passphrases used by repository operations.

`config_entries.value` stores the reference for sensitive values, not the resolved secret. API responses and CLI output mask sensitive values. Worker and runtime services resolve secrets at use time through a `SecretResolver` abstraction.

Minimal backend contract:

```go
type SecretResolver interface {
    Resolve(ctx context.Context, ref string) (SecretValue, error)
}
```

Resolution failures must be explicit, auditable and must not log the unresolved secret value if the value is not a reference.

## Rationale

The project needs a concrete secret contract before config import and repository/auth adapters are implemented. An environment-backed resolver is enough for local development, CI and early server deployments while preserving an extension point for stronger secret stores.

## Consequences

- `thelper-ctl -reconfigure` validates sensitive keys and rejects literal secrets unless a future ADR explicitly allows encrypted local storage.
- `GET /api/config` returns masked sensitive values.
- `PUT /api/config` uses the same sensitive key validation and masking contract as `thelper-ctl -reconfigure`.
- Job payloads, result payloads, job events, audit payloads and logs must not include resolved secrets.
- Local PostgreSQL development uses environment variables referenced through `secretref://env/...`.
