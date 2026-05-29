-- +goose Up
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
DROP INDEX IF EXISTS repositories_superseded_by_idx;
DROP INDEX IF EXISTS repositories_discovery_source_idx;
DROP INDEX IF EXISTS repositories_status_idx;
DROP INDEX IF EXISTS repositories_local_path_idx;
DROP INDEX IF EXISTS repositories_provider_instance_full_path_idx;
DROP INDEX IF EXISTS repositories_provider_host_full_path_idx;
DROP INDEX IF EXISTS repositories_generic_local_identity_uidx;
DROP INDEX IF EXISTS repositories_provider_identity_uidx;

PRAGMA legacy_alter_table=ON;
ALTER TABLE repositories RENAME TO repositories_stage05_hardening_old;
PRAGMA legacy_alter_table=OFF;

CREATE TABLE repositories (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider_instance_id TEXT,
  provider TEXT NOT NULL CHECK (provider IN ('gitlab', 'github', 'bitbucket', 'azure_devops', 'generic')),
  provider_host TEXT NOT NULL,
  full_path TEXT NOT NULL,
  clone_url TEXT,
  default_branch TEXT,
  root_path_id TEXT,
  target_directory TEXT,
  local_path TEXT,
  auth_type TEXT CHECK (auth_type IS NULL OR auth_type IN ('ssh', 'https', 'token')),
  default_credential_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'superseded', 'disabled')),
  discovery_source TEXT NOT NULL CHECK (discovery_source IN ('filesystem', 'provider', 'clone', 'manual')),
  superseded_by_repository_id TEXT,
  identity_confirmed_at TEXT,
  auto_sync_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_sync_enabled IN (0, 1)),
  webhook_enabled INTEGER NOT NULL DEFAULT 0 CHECK (webhook_enabled IN (0, 1)),
  poll_interval TEXT,
  last_pull_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (root_path_id) REFERENCES root_paths(id) ON DELETE RESTRICT,
  FOREIGN KEY (provider_instance_id) REFERENCES repository_provider_instances(id) ON DELETE SET NULL,
  FOREIGN KEY (default_credential_id) REFERENCES repository_credentials(id) ON DELETE SET NULL,
  FOREIGN KEY (superseded_by_repository_id) REFERENCES repositories(id) ON DELETE SET NULL
);

INSERT INTO repositories (
  id, name, provider_instance_id, provider, provider_host, full_path, clone_url,
  default_branch, root_path_id, target_directory, local_path, auth_type,
  default_credential_id, status, discovery_source, superseded_by_repository_id,
  identity_confirmed_at, auto_sync_enabled, webhook_enabled, poll_interval,
  last_pull_at, last_error, created_at, updated_at
)
SELECT
  r.id, r.name,
  CASE WHEN r.provider_instance_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM repository_provider_instances p WHERE p.id = r.provider_instance_id
  ) THEN r.provider_instance_id ELSE NULL END,
  r.provider, r.provider_host, r.full_path, r.clone_url,
  r.default_branch, r.root_path_id, r.target_directory, r.local_path, r.auth_type,
  CASE WHEN r.default_credential_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM repository_credentials c WHERE c.id = r.default_credential_id
  ) THEN r.default_credential_id ELSE NULL END,
  r.status, r.discovery_source, r.superseded_by_repository_id,
  r.identity_confirmed_at, r.auto_sync_enabled, r.webhook_enabled, r.poll_interval,
  r.last_pull_at, r.last_error, r.created_at, r.updated_at
FROM repositories_stage05_hardening_old r;

DROP TABLE repositories_stage05_hardening_old;

CREATE UNIQUE INDEX IF NOT EXISTS repositories_provider_identity_uidx
  ON repositories (provider, provider_host, full_path)
  WHERE NOT (provider = 'generic' AND provider_host = 'local');
CREATE UNIQUE INDEX IF NOT EXISTS repositories_generic_local_identity_uidx
  ON repositories (provider, provider_host, root_path_id, full_path)
  WHERE provider = 'generic' AND provider_host = 'local';
CREATE INDEX IF NOT EXISTS repositories_provider_host_full_path_idx
  ON repositories (provider_host, full_path);
CREATE INDEX IF NOT EXISTS repositories_provider_instance_full_path_idx
  ON repositories (provider_instance_id, full_path);
CREATE INDEX IF NOT EXISTS repositories_local_path_idx
  ON repositories (local_path);
CREATE INDEX IF NOT EXISTS repositories_root_target_directory_idx
  ON repositories (root_path_id, target_directory);
CREATE INDEX IF NOT EXISTS repositories_root_local_path_idx
  ON repositories (root_path_id, local_path);
CREATE INDEX IF NOT EXISTS repositories_status_idx
  ON repositories (status);
CREATE INDEX IF NOT EXISTS repositories_discovery_source_idx
  ON repositories (discovery_source);
CREATE INDEX IF NOT EXISTS repositories_superseded_by_idx
  ON repositories (superseded_by_repository_id);
CREATE INDEX IF NOT EXISTS repository_credentials_provider_instance_auth_type_idx
  ON repository_credentials (provider_instance_id, auth_type);
PRAGMA foreign_keys=ON;

-- +goose Down
-- +goose NO TRANSACTION
PRAGMA foreign_keys=OFF;
DROP INDEX IF EXISTS repository_credentials_provider_instance_auth_type_idx;
DROP INDEX IF EXISTS repositories_superseded_by_idx;
DROP INDEX IF EXISTS repositories_discovery_source_idx;
DROP INDEX IF EXISTS repositories_status_idx;
DROP INDEX IF EXISTS repositories_root_local_path_idx;
DROP INDEX IF EXISTS repositories_root_target_directory_idx;
DROP INDEX IF EXISTS repositories_local_path_idx;
DROP INDEX IF EXISTS repositories_provider_instance_full_path_idx;
DROP INDEX IF EXISTS repositories_provider_host_full_path_idx;
DROP INDEX IF EXISTS repositories_generic_local_identity_uidx;
DROP INDEX IF EXISTS repositories_provider_identity_uidx;

PRAGMA legacy_alter_table=ON;
ALTER TABLE repositories RENAME TO repositories_stage05_hardening_new;
PRAGMA legacy_alter_table=OFF;

CREATE TABLE repositories (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider_instance_id TEXT,
  provider TEXT NOT NULL CHECK (provider IN ('gitlab', 'github', 'bitbucket', 'azure_devops', 'generic')),
  provider_host TEXT NOT NULL,
  full_path TEXT NOT NULL,
  clone_url TEXT,
  default_branch TEXT,
  root_path_id TEXT,
  target_directory TEXT,
  local_path TEXT,
  auth_type TEXT CHECK (auth_type IS NULL OR auth_type IN ('ssh', 'https', 'token')),
  default_credential_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'superseded', 'disabled')),
  discovery_source TEXT NOT NULL CHECK (discovery_source IN ('filesystem', 'provider', 'clone', 'manual')),
  superseded_by_repository_id TEXT,
  identity_confirmed_at TEXT,
  auto_sync_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_sync_enabled IN (0, 1)),
  webhook_enabled INTEGER NOT NULL DEFAULT 0 CHECK (webhook_enabled IN (0, 1)),
  poll_interval TEXT,
  last_pull_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (root_path_id) REFERENCES root_paths(id) ON DELETE RESTRICT,
  FOREIGN KEY (superseded_by_repository_id) REFERENCES repositories(id) ON DELETE SET NULL
);

INSERT INTO repositories (
  id, name, provider_instance_id, provider, provider_host, full_path, clone_url,
  default_branch, root_path_id, target_directory, local_path, auth_type,
  default_credential_id, status, discovery_source, superseded_by_repository_id,
  identity_confirmed_at, auto_sync_enabled, webhook_enabled, poll_interval,
  last_pull_at, last_error, created_at, updated_at
)
SELECT
  id, name, provider_instance_id, provider, provider_host, full_path, clone_url,
  default_branch, root_path_id, target_directory, local_path, auth_type,
  default_credential_id, status, discovery_source, superseded_by_repository_id,
  identity_confirmed_at, auto_sync_enabled, webhook_enabled, poll_interval,
  last_pull_at, last_error, created_at, updated_at
FROM repositories_stage05_hardening_new;

DROP TABLE repositories_stage05_hardening_new;

CREATE UNIQUE INDEX IF NOT EXISTS repositories_provider_identity_uidx
  ON repositories (provider, provider_host, full_path)
  WHERE NOT (provider = 'generic' AND provider_host = 'local');
CREATE UNIQUE INDEX IF NOT EXISTS repositories_generic_local_identity_uidx
  ON repositories (provider, provider_host, root_path_id, full_path)
  WHERE provider = 'generic' AND provider_host = 'local';
CREATE INDEX IF NOT EXISTS repositories_provider_host_full_path_idx
  ON repositories (provider_host, full_path);
CREATE INDEX IF NOT EXISTS repositories_provider_instance_full_path_idx
  ON repositories (provider_instance_id, full_path);
CREATE INDEX IF NOT EXISTS repositories_local_path_idx
  ON repositories (local_path);
CREATE INDEX IF NOT EXISTS repositories_root_target_directory_idx
  ON repositories (root_path_id, target_directory);
CREATE INDEX IF NOT EXISTS repositories_root_local_path_idx
  ON repositories (root_path_id, local_path);
CREATE INDEX IF NOT EXISTS repositories_status_idx
  ON repositories (status);
CREATE INDEX IF NOT EXISTS repositories_discovery_source_idx
  ON repositories (discovery_source);
CREATE INDEX IF NOT EXISTS repositories_superseded_by_idx
  ON repositories (superseded_by_repository_id);
PRAGMA foreign_keys=ON;
