-- +goose Up
CREATE TABLE IF NOT EXISTS repository_provider_instances (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('github', 'generic')),
  provider_host TEXT NOT NULL,
  api_base_url TEXT,
  web_base_url TEXT,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (provider, provider_host)
);

CREATE INDEX IF NOT EXISTS repository_provider_instances_provider_host_idx
  ON repository_provider_instances (provider, provider_host);

CREATE TABLE IF NOT EXISTS repository_credentials (
  id TEXT PRIMARY KEY,
  provider_instance_id TEXT NOT NULL,
  name TEXT NOT NULL,
  auth_type TEXT NOT NULL CHECK (auth_type IN ('ssh', 'https', 'token')),
  secret_ref TEXT NOT NULL,
  username TEXT,
  usages TEXT NOT NULL,
  scope_hint TEXT,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (provider_instance_id, name),
  FOREIGN KEY (provider_instance_id) REFERENCES repository_provider_instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS repository_credentials_provider_instance_idx
  ON repository_credentials (provider_instance_id, enabled);

CREATE TABLE IF NOT EXISTS repository_operation_reservations (
  id TEXT PRIMARY KEY,
  reservation_key TEXT NOT NULL,
  owner TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('held', 'released', 'expired')),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  released_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS repository_operation_reservations_held_key_uidx
  ON repository_operation_reservations (reservation_key)
  WHERE status = 'held';

CREATE INDEX IF NOT EXISTS repository_operation_reservations_cleanup_idx
  ON repository_operation_reservations (status, expires_at, released_at, created_at, id);

UPDATE system_metadata SET value = 'stage-05', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS repository_operation_reservations_cleanup_idx;
DROP INDEX IF EXISTS repository_operation_reservations_held_key_uidx;
DROP TABLE IF EXISTS repository_operation_reservations;
DROP INDEX IF EXISTS repository_credentials_provider_instance_idx;
DROP TABLE IF EXISTS repository_credentials;
DROP INDEX IF EXISTS repository_provider_instances_provider_host_idx;
DROP TABLE IF EXISTS repository_provider_instances;

UPDATE system_metadata SET value = 'stage-04', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';
