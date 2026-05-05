-- +goose Up
CREATE TABLE IF NOT EXISTS config_entries (
  id TEXT PRIMARY KEY,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  value_type TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT 'system',
  version INTEGER NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL,
  updated_by TEXT NOT NULL,
  UNIQUE (key, scope)
);

CREATE TABLE IF NOT EXISTS storage_profiles (
  id TEXT PRIMARY KEY,
  slot TEXT NOT NULL,
  provider TEXT NOT NULL,
  engine_flavor TEXT,
  status TEXT NOT NULL,
  config_payload TEXT NOT NULL,
  database_fingerprint TEXT NOT NULL,
  last_migrated_from_profile_id TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS storage_profiles_one_active_current
  ON storage_profiles (slot)
  WHERE slot = 'current' AND status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS storage_profiles_one_migration_slot
  ON storage_profiles (slot)
  WHERE slot = 'migration';

CREATE TABLE IF NOT EXISTS storage_provider_settings (
  id TEXT PRIMARY KEY,
  storage_profile_id TEXT NOT NULL REFERENCES storage_profiles(id),
  provider TEXT NOT NULL,
  workers_concurrency INTEGER NOT NULL,
  worker_process_limit INTEGER NOT NULL,
  busy_timeout TEXT NOT NULL,
  lease_duration TEXT NOT NULL,
  heartbeat_interval TEXT NOT NULL,
  sqlite_journal_mode TEXT,
  sqlite_foreign_keys BOOLEAN,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS module_states (
  id TEXT PRIMARY KEY,
  module_name TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL,
  pid INTEGER,
  host TEXT,
  details TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ignore_rules (
  id TEXT PRIMARY KEY,
  scope_type TEXT NOT NULL,
  scope_id TEXT,
  pattern TEXT NOT NULL,
  origin TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (scope_type, scope_id, pattern)
);

UPDATE system_metadata SET value = 'stage-02', updated_at = now()
  WHERE key = 'schema_version';

-- +goose Down
DROP TABLE IF EXISTS ignore_rules;
DROP TABLE IF EXISTS module_states;
DROP TABLE IF EXISTS storage_provider_settings;
DROP INDEX IF EXISTS storage_profiles_one_migration_slot;
DROP INDEX IF EXISTS storage_profiles_one_active_current;
DROP TABLE IF EXISTS storage_profiles;
DROP TABLE IF EXISTS config_entries;

UPDATE system_metadata SET value = 'stage-01', updated_at = now()
  WHERE key = 'schema_version';
