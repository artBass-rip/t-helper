# Configuration

## Source of Truth

After the initial import, the source of truth for runtime configuration is the database.

Files are used only as input for `thelper-ctl -reconfigure`:

- `config.json`
- `.t-helper.ignore`

`config.example.json` is shipped as a valid reference for the structure of `config.json`.

`thelper` must not read these files as the runtime source of truth after a successful import.

Storage backend settings are a special part of configuration. `config.json` may
be used for initial reading and for preparing a migration target, but runtime
selects the active database from records in the storage configuration table. This
table stores at least two profile slots:

- `current` - the database currently used by the active runtime;
- `migration` - the database that can be migrated to through `thelper-ctl -migrate-db`.

`thelper-ctl -reconfigure` may update runtime config and profile slot `migration`,
but it does not switch the active database. Switching from `current` to
`migration` is allowed only after a successful `thelper-ctl -migrate-db`, which
creates/updates the schema, migrates data, verifies the result and updates
profile statuses. Information about the old database is kept in the storage
configuration table; the old database itself is not deleted automatically and may
be used by an administrator for manual rollback.

## Example `config.json`

Comments in examples are allowed only in documentation. The file read by `thelper-ctl -reconfigure` must be valid JSON without comments.

```json
{
  "system_settings": {
    "app_name": "t-helper",
    "version": "1.0.0",
    "mode": "server"
  },
  "database": {
    "database_type": "sqlite",
    "database_path": "/var/lib/t-helper/t-helper.db"
  },
  "external_databases": {
    "enabled": false,
    "provider": "postgresql",
    "engine_flavor": "standard",
    "host": "postgres.example.internal",
    "port": 5432,
    "username": "secretref://env/THELPER_POSTGRES_USER",
    "password": "secretref://env/THELPER_POSTGRES_PASSWORD",
    "database_name": "t_helper"
  },
  "scanning": {
    "global_scan": [
      {
        "name": "Platform Terraform",
        "root_path": "/srv/t-helper/scan-roots/platform",
        "schedule": true,
        "frequency": "daily"
      },
      {
        "name": "Application Terraform",
        "root_path": "/srv/t-helper/scan-roots/applications",
        "schedule": true,
        "frequency": "weekly"
      }
    ],
    "security_scan": {
      "modules": [
        "trivy"
      ]
    },
    "toolchain": {
      "version_policy": "certified_only",
      "profile_paths": []
    }
  },
  "repositories": {
    "default_auth_type": "ssh",
    "poll_interval_default": "15m",
    "auto_sync_default": false
  },
  "security": {
    "active_rule_set_id": null
  },
  "api": {
    "listen_address": "127.0.0.1:8080"
  },
  "auth": {
    "local_enabled": true
  },
  "workers": {
    "enabled": true,
    "concurrency": 1
  },
  "modules": {
    "enabled": [
      "core",
      "worker-runtime",
      "config-manager",
      "module-runtime",
      "status-monitor",
      "global-scanner",
      "project-scanner",
      "security-validator",
      "repository-manager",
      "auth",
      "web"
    ]
  },
  "logging": {
    "level": "info",
    "format": "json",
    "log_path": "/var/log/t-helper"
  }
}
```

## `config.json` Sections

### `system_settings`

- `app_name` - application name; `t-helper` for the standard distribution;
- `version` - configuration schema/distribution version;
- `mode` - launch mode, `server` or `local`.

### `database`

Describes the internal storage used when `external_databases.enabled = false`.

- `database_type` - `sqlite`;
- `database_path` - path to the internal storage directory or file.

### `external_databases`

Describes the external storage target. If `enabled = true`, all required fields
for the external connection must be set. In Stage 02 this creates or updates the
`migration` profile; runtime starts using the external database only after a
successful `thelper-ctl -migrate-db` and a subsequent start with the promoted
`current` profile.

- `enabled` - enables the external storage backend;
- `provider` - `postgresql`, `mysql` or `mssql`;
- `engine_flavor` - optional operational hint for compatible managed engines; supported values: `standard`, `aurora`;
- `host` - IP address or FQDN;
- `port` - provider TCP port;
- `username` - username or encoded secret representation when a secret codec is used;
- `password` - password or secret reference/encoded representation;
- `database_name` - database name.

`provider = postgresql` covers PostgreSQL-compatible engines, including Amazon Aurora PostgreSQL. `provider = mysql` covers MySQL-compatible engines, including Amazon Aurora MySQL. Aurora is not a separate provider/dialect: it uses `postgresql` or `mysql` migrations/adapters.

`provider = mssql` is intended for native Microsoft SQL Server-compatible engines. Babelfish for Aurora PostgreSQL is not considered equivalent to the `mssql` adapter without a separate compatibility decision.

All database providers are implemented as pluggable storage adapter libraries. An unknown provider must produce `validation_error` without partially applying configuration.

`database` and `external_databases` in the import file describe the initial
`current` profile and optional `migration` profile. For an empty installation
with `external_databases.enabled = true`, `database` remains the initial
`current` profile, while `external_databases` creates the `migration` target.
They are not reloadable runtime settings and must not switch the active storage
backend without `thelper-ctl -migrate-db`.

### `workers`

Describes separate worker processes that execute background jobs.

- `enabled` - enables processing queued jobs through `thelper-worker`;
- `concurrency` - maximum number of jobs executed concurrently by one worker process.

`thelper` must not execute long-running jobs inline. It creates jobs and hands them off to separate `thelper-worker` processes.
If `workers.enabled = false`, `thelper-worker` exits without claiming queued
jobs; existing jobs remain persisted for a later enabled worker or explicit
operator action.

Worker execution defaults are provider-specific. `workers.concurrency` in
top-level config remains a compatibility/default value for the current active
provider, but effective limits, busy timeout, lease defaults and concurrency
policy must be stored and applied separately for each database provider/profile.

Minimum MVP defaults:

- `sqlite`: one active worker process, `concurrency = 1`, `journal_mode = WAL`, `foreign_keys = ON`, `busy_timeout = 5s`;
- `postgresql`: multiple worker processes allowed, concurrency is installation-specific and can be increased after load testing.

`thelper-worker` enforces the SQLite process limit with a local worker lock file
under `.artifacts/runtime` by default. The lock is keyed by the active database
fingerprint and is released on normal worker shutdown; stale locks left by dead
processes are replaced.

Changing worker settings for one provider/profile must not change settings for
another provider/profile. For example, setting PostgreSQL concurrency before
migration must not change SQLite local-mode concurrency.

### `scanning`

Describes global scanning settings. Project-level scan and security/validation scan settings are stored per project and must not be set as global default parameters in `config.json`.

- `global_scan` - list of root paths for global scanning;
- `global_scan[].root_path` - absolute root path traversed by `global-scanner`;
- `global_scan[].schedule` - enables scheduled runs for a specific path;
- `global_scan[].frequency` - `daily`, `weekly` or `monthly`;
- `security_scan.modules` - list of available security/validation modules and policy engines that can be attached in individual project settings. The MVP example includes only the mandatory `trivy`; `gitleaks`, `checkov`, `opa` and `conftest` are extension modules outside mandatory MVP acceptance.
- `scanning.toolchain.version_policy` - admission policy for external CLI tool versions: `certified_only`, `compatible_range` or `latest_best_effort`;
- `scanning.toolchain.profile_paths` - optional local directories/files with additional tool profile files from ADR 0018. Profiles imported from these paths must pass validation before activation.

`global_scan` is the canonical input configuration field name. Inside storage, these records are mapped to the `root_paths` entity.

Scan and clone use the same materialized list of `root_paths`.
During clone, the user selects an existing root path imported from
`global_scan[].root_path` or created through the API, or creates a new root path.
If clone targets a new root path, it is saved as a new `root_path` with
`source = api`; `scanning.global_scan` as the external config source is not
rewritten by repository-manager.

### `repositories`

This section contains default parameters for repository operations and does not define a separate `repo_root`.

- `default_auth_type` - default transport/auth hint for clone/pull/sync, not a credential source;
- `poll_interval_default` - default polling sync interval;
- `auto_sync_default` - default auto sync for new repository cards.

Provider hosts and repository credentials are managed through `repository_provider_instances` and `repository_credentials`, not through `config.json`. Credential secret values must be referenced through `secretref://...` and are resolved by workers at use time.

### `modules`

This section contains the list of registered runtime modules that must be active in the current installation.

The initial module registry is created by a seed step in foundation stages and contains:

- `core`
- `worker-runtime`
- `config-manager`
- `module-runtime`
- `status-monitor`
- `global-scanner`
- `repository-manager`
- `project-scanner`
- `security-validator`
- `auth`
- `web`

`modules.enabled` may reference only registered modules. An unknown module name must produce `validation_error` without partially applying configuration.

A module may be registered before full implementation. In that case, `GET /api/modules` returns it with state `unavailable`, and `restart`/`reload` returns a controlled error instead of `404` or panic.

## `.t-helper.ignore`

Rules are applied relative to `root_path`.

```gitignore
.terraform/
**/.git/
**/node_modules/
**/.cache/
```

The MVP matcher may be exclude-only. Rules of the form `!pattern` must be imported and stored without data loss, but are applied only after full `.gitignore` semantics are implemented.

## Reloadability

Reloadable without a full restart:

- `scanning.global_scan`, applied to new global scan jobs
- `scanning.security_scan.modules`, applied to new project security/validation jobs
- `repositories.default_auth_type`
- `repositories.poll_interval_default`
- `repositories.auto_sync_default`
- `security.active_rule_set_id`
- `logging.level`
- `logging.format`
- `logging.log_path`, if the logging backend supports reopen without restart
- `modules.enabled`, if the module supports graceful start/stop

Require restart of a specific module:

- `api.listen_address`
- `auth.local_enabled`
- `workers.enabled`
- `workers.concurrency`
- provider-specific auth adapter parameters
- `system_settings.mode`

Not applied through reload/restart:

- `database.database_type`
- `database.database_path`
- `external_databases.*`

These keys update only storage profile metadata for initial bootstrap or the
`migration` slot. Active database switching is performed only through
`thelper-ctl -migrate-db`.

`thelper-ctl -reload` must explicitly return the list of accepted reloadable
parameters, the list of parameters actually applied in Stage 02 and the list of
parameters requiring `thelper-ctl -restart <module>` or a full service restart.
A reloadable key must not appear in `applied_keys` if the current runtime has
not yet implemented its application without restart. Explicit unknown reload
keys are returned in `failed_keys` and must not be silently treated as applied.

## Validation

`thelper-ctl -reconfigure` and `PUT /api/config` use a strict schema contract:

- unknown top-level or nested keys must return `validation_error`;
- malformed JSON, trailing payload after the first JSON object and `null` instead
  of a config object must return `validation_error`;
- deprecated/legacy aliases are not accepted;
- `scanning.global_scan` is the only allowed key for global scan roots;
- `scanning.global_scann`, `globalScan`, `global_scan_roots`, `scan_roots` and any other aliases must be rejected as unknown keys;
- validation errors must not partially change `config_entries`,
  `ignore_rules` or runtime state;
- `PUT /api/config` does not accept `.t-helper.ignore` payload and therefore does
  not delete previously imported system `ignore_rules`; rules are cleared through
  the existing empty `.t-helper.ignore` during `thelper-ctl -reconfigure` or through
  Stage 04 ignore-rules API.

Minimum rules:

- `system_settings.app_name` must not be empty;
- `system_settings.mode` accepts `server` or `local`;
- `database.database_type` accepts `sqlite`;
- `database.database_path` must be a normalized path;
- if `external_databases.enabled = true`, `provider`, `host`, `port`, `username`, `password` and `database_name` are required;
- `external_databases.provider` accepts `postgresql`, `mysql` or `mssql`;
- `external_databases.engine_flavor`, if set, accepts `standard` or `aurora`;
- `external_databases.port` must be a positive TCP port;
- `secretref://env/...` in MVP means an environment variable reference, not a literal value to store in the database;
- `scanning.global_scan[].root_path` must be an absolute normalized path;
- `scanning.global_scan[].schedule` must be boolean;
- `scanning.global_scan[].frequency` accepts `daily`, `weekly` or `monthly`;
- `scanning.security_scan.modules` must contain non-empty unique module names;
- `scanning.toolchain.version_policy` accepts `certified_only`, `compatible_range` or `latest_best_effort`; default MVP value is `certified_only`;
- `scanning.toolchain.profile_paths[]` must contain absolute normalized local paths, if set;
- `repositories.default_auth_type` accepts `ssh`, `https` or `token` and is not a credential source;
- `repositories.poll_interval_default` must be a positive duration;
- `workers.enabled` must be boolean;
- `workers.concurrency` must be a positive integer;
- for `sqlite`, effective `workers.concurrency` must be `1`, and attempting to apply a larger value to an active SQLite profile must return `sqlite_worker_concurrency_unsupported`;
- `modules.enabled` must contain only names from the initial module registry or explicitly registered extension modules;
- `api.listen_address` must be a valid host:port;
- `logging.level` accepts `debug`, `info`, `warn`, `error`;
- `logging.format` accepts `json` or `text`;
- `logging.log_path` must be a normalized directory path;
- sensitive keys must use `secretref://env/...`; literal values for sensitive keys must return `validation_error`;
- secrets and tokens must not be stored in plaintext in `config_entries.value`.

Invalid configuration must not be partially applied: import and reload are atomic with respect to one change set.

Sensitive keys:

- `external_databases.username`
- `external_databases.password`
- provider tokens and Git HTTPS tokens
- webhook secrets
- auth provider client secrets
- private keys or passphrases

`config_entries.value` stores the secret reference, not the resolved secret. Resolved secrets must not appear in `jobs.payload`, `jobs.result_payload`, `job_events.payload`, `workflow_statuses.summary_payload`, `audit_log.payload` or logs.

## Masked config output

`GET /api/config` returns the active runtime configuration with masked sensitive values.

API response must not disclose resolved secret values. `secretref://env/NAME`
is not a raw secret, but the environment variable name may reveal infrastructure
details, so output mode depends on permissions:

- `system_admin` and subjects with `system.config.read` may see the full secret reference, for example `secretref://env/THELPER_POSTGRES_PASSWORD`, but never the resolved value;
- `viewer`, object-scoped readers and responses allowed only through `system.runtime.read` receive masked metadata without the variable name;
- audit, job payloads, job results, workflow payloads and logs never contain resolved values and should prefer masked metadata over full refs unless the event is explicitly administrative.

Allowed masked form:

```json
{
  "masked": true,
  "ref_type": "env"
}
```

Raw `secretref://env/...` may be stored in `config_entries.value`, but API output must avoid disclosing the resolved secret and must not show literal secret values.

## Storage migration command

`thelper-ctl -migrate-db` performs controlled migration from the `current` profile to
`migration` profile.

Minimum Stage 02 contract:

- check availability of the migration DB, provider, credentials and permissions;
- apply schema migrations to the migration DB;
- migrate Stage 02-owned runtime data: `config_entries`,
  `storage_profiles`, `storage_provider_settings`, `module_states`,
  `ignore_rules` and system migration metadata;
- do not migrate active transient locks as active state;
- verify logical migration version, FK integrity and basic counts/checksums;
- update profile statuses: migration becomes `current`, and the previous current is kept as a historical/rollback candidate;
- support subsequent migration targets after a successful switch without overwriting the active `current` profile;
- do not delete the old database automatically;
- require runtime restart after a successful switch.

If migration does not complete successfully, the active `current` profile does not change.

Later stages must extend this same migration contract when they introduce their
own persistent tables. For example, Stage 03 extends it for jobs/workflows,
Stage 06 for findings/rule sets and Stage 07 for auth/RBAC/audit, including
audit event `storage.migration_completed` once audit storage exists.
