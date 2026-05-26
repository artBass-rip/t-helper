-- +goose Up
CREATE TABLE IF NOT EXISTS repository_provider_instances (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('github', 'generic')),
  provider_host TEXT NOT NULL,
  api_base_url TEXT,
  web_base_url TEXT,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (provider, provider_host)
);

CREATE INDEX IF NOT EXISTS repository_provider_instances_provider_host_idx
  ON repository_provider_instances (provider, provider_host);

CREATE TABLE IF NOT EXISTS repository_credentials (
  id TEXT PRIMARY KEY,
  provider_instance_id TEXT NOT NULL REFERENCES repository_provider_instances(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  auth_type TEXT NOT NULL CHECK (auth_type IN ('ssh', 'https', 'token')),
  secret_ref TEXT NOT NULL,
  username TEXT,
  usages TEXT NOT NULL,
  scope_hint TEXT,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (provider_instance_id, name)
);

CREATE INDEX IF NOT EXISTS repository_credentials_provider_instance_idx
  ON repository_credentials (provider_instance_id, enabled);

UPDATE system_metadata SET value = 'stage-05', updated_at = now()
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS repository_credentials_provider_instance_idx;
DROP TABLE IF EXISTS repository_credentials;
DROP INDEX IF EXISTS repository_provider_instances_provider_host_idx;
DROP TABLE IF EXISTS repository_provider_instances;

UPDATE system_metadata SET value = 'stage-04', updated_at = now()
  WHERE key = 'schema_version';
