# ADR 0014: Local password hashing

## Status

Accepted.

## Decision

Local authentication stores password verifiers using `Argon2id` encoded as PHC strings.

Default production parameters:

```text
algorithm: argon2id
version: 19
memory: 64 MiB
iterations: 3
parallelism: 4
salt_length: at least 16 bytes
hash_length: at least 32 bytes
format: PHC string
```

Example PHC shape:

```text
$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
```

Salt is generated randomly per password by the password hasher and encoded in the PHC string. Pepper is not required in the MVP.

Local credentials are stored separately from the shared `users` identity entity in `local_user_credentials`. Raw passwords are never persisted.

Password hash upgrade policy:

- current Argon2id parameters are configuration-owned defaults;
- stored PHC parameters are inspected on successful local login;
- if stored parameters are weaker than current defaults, the password is rehashed after successful verification;
- dev/test may override hashing parameters, but production defaults remain the accepted baseline.

Initial password and reset policy:

- first-run bootstrap admin user is created automatically when `thelper` starts against an empty auth state;
- bootstrap username and password are random latin alphanumeric strings with length 16 each;
- bootstrap credentials are displayed once in the first UI that connects to the runtime and in stdout;
- the display must warn that bootstrap credentials expire after 24 hours;
- if the bootstrap user is not used within 24 hours, it is deleted and no second bootstrap user is created automatically;
- if auth was not configured before bootstrap expiry, recovery requires complete data deletion and a new empty first run;
- temporary passwords are shown or delivered once and are not persisted as plaintext;
- password reset tokens are stored only as hashes;
- reset tokens have a TTL and one-time-use semantics;
- reset completion sets `password_must_change = true` unless an explicit administrative policy says otherwise.

Lockout policy:

- after 5 consecutive failed local password attempts, the local credential is locked for 15 minutes;
- successful login resets `failed_attempt_count`;
- failed login responses must be generic and must not reveal whether the username exists.

Logging, payload and audit policy:

- raw passwords, password hashes, password reset tokens and reset token hashes must never be returned by API responses;
- raw passwords, password hashes, password reset tokens and reset token hashes must never be written to logs, job payloads, result payloads, job events, workflow summaries or audit payloads;
- audit events may record that a password was changed, reset or locked, but not the secret material.

## Rationale

Local auth is part of the MVP. A fixed password hashing policy prevents incompatible implementations across CLI setup, API auth and future UI flows.

`Argon2id` is a modern memory-hard password hashing algorithm suitable for on-premise deployments. PHC strings keep algorithm and parameters versioned with each stored hash, enabling rehash-on-login without schema churn.

Keeping local credentials outside `users` keeps the shared identity model compatible with external auth providers and SCIM.

## Consequences

- Stage 07 migrations add `local_user_credentials` and `password_reset_tokens`.
- `users` remains the shared identity entity and does not store password hashes.
- Local auth implementation must support Argon2id PHC verification and rehash-on-login.
- API DTOs must not expose password hashes, reset tokens or reset token hashes.
- Tests must cover PHC format, no plaintext persistence, no secret leakage, rehash-on-login and lockout behavior.
