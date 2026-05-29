# ADR 0008: Global scan configuration key

## Status

Accepted.

## Decision

The external configuration key for global scan roots is:

```text
scanning.global_scan
```

This is the only supported MVP spelling for the global scan roots configuration key.

Internal implementation names must use the domain concept `root_paths`:

- storage table: `root_paths`;
- API: `/api/root-paths`;
- application/domain naming: `root_paths`;
- config import source key: `scanning.global_scan`.

No parallel alias is introduced in the MVP. If a future release adds alias support, it must define explicit import precedence, validation and migration behavior.

Strict config import must reject unknown keys and aliases, including previous draft spellings.

## Rationale

Normalizing the key before code scaffolding avoids carrying a misspelled external contract into migrations, config import/export tests and user-facing examples.

Using `root_paths` internally keeps domain, storage and API names clear and avoids leaking the configuration spelling into implementation packages.

## Consequences

- `thelper-ctl -reconfigure` reads `scanning.global_scan` and maps entries to `root_paths`.
- `GET /api/config` returns the canonical external key `scanning.global_scan`.
- `GET /api/root-paths` and `PUT /api/root-paths` remain the operational API for root paths.
- Tests must assert that config import/export preserves `scanning.global_scan`.
- Tests must assert that non-canonical aliases are rejected.
