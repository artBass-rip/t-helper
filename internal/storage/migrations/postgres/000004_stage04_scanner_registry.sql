-- +goose Up
ALTER TABLE ignore_rules ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS ignore_rules_scope_order_idx
  ON ignore_rules (scope_type, scope_id, sort_order, id);

CREATE TABLE IF NOT EXISTS root_paths (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  enabled BOOLEAN NOT NULL DEFAULT true,
  schedule_enabled BOOLEAN NOT NULL DEFAULT false,
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS root_paths_enabled_path_idx
  ON root_paths (enabled, path);

CREATE TABLE IF NOT EXISTS environments (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  code TEXT NOT NULL UNIQUE,
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS repositories (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider_instance_id TEXT,
  provider TEXT NOT NULL CHECK (provider IN ('gitlab', 'github', 'bitbucket', 'azure_devops', 'generic')),
  provider_host TEXT NOT NULL,
  full_path TEXT NOT NULL,
  clone_url TEXT,
  default_branch TEXT,
  root_path_id TEXT REFERENCES root_paths(id) ON DELETE RESTRICT,
  target_directory TEXT,
  local_path TEXT,
  auth_type TEXT CHECK (auth_type IS NULL OR auth_type IN ('ssh', 'https', 'token')),
  default_credential_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'superseded', 'disabled')),
  discovery_source TEXT NOT NULL CHECK (discovery_source IN ('filesystem', 'provider', 'clone', 'manual')),
  superseded_by_repository_id TEXT REFERENCES repositories(id) ON DELETE SET NULL,
  identity_confirmed_at TIMESTAMPTZ,
  auto_sync_enabled BOOLEAN NOT NULL DEFAULT false,
  webhook_enabled BOOLEAN NOT NULL DEFAULT false,
  poll_interval TEXT,
  last_pull_at TIMESTAMPTZ,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

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
CREATE INDEX IF NOT EXISTS repositories_status_idx
  ON repositories (status);
CREATE INDEX IF NOT EXISTS repositories_discovery_source_idx
  ON repositories (discovery_source);
CREATE INDEX IF NOT EXISTS repositories_superseded_by_idx
  ON repositories (superseded_by_repository_id);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  root_path_id TEXT NOT NULL REFERENCES root_paths(id) ON DELETE RESTRICT,
  terraform_marker TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'disabled')),
  repository_id TEXT REFERENCES repositories(id) ON DELETE SET NULL,
  environment_id TEXT REFERENCES environments(id) ON DELETE SET NULL,
  default_workspace_id TEXT,
  detected_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (root_path_id, relative_path)
);

CREATE INDEX IF NOT EXISTS projects_root_path_status_idx
  ON projects (root_path_id, status);
CREATE INDEX IF NOT EXISTS projects_repository_id_idx
  ON projects (repository_id);
CREATE INDEX IF NOT EXISTS projects_environment_id_idx
  ON projects (environment_id);
CREATE INDEX IF NOT EXISTS projects_default_workspace_id_idx
  ON projects (default_workspace_id);
CREATE INDEX IF NOT EXISTS projects_status_updated_at_idx
  ON projects (status, updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  is_default BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id, name)
);

CREATE INDEX IF NOT EXISTS workspaces_project_id_idx
  ON workspaces (project_id);
CREATE INDEX IF NOT EXISTS workspaces_environment_id_idx
  ON workspaces (environment_id);

ALTER TABLE projects
  ADD CONSTRAINT projects_default_workspace_id_fk
  FOREIGN KEY (default_workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS project_links (
  id TEXT PRIMARY KEY,
  source_project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  target_project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  link_type TEXT NOT NULL CHECK (link_type IN ('same_repository')),
  repository_id TEXT REFERENCES repositories(id) ON DELETE SET NULL,
  detected_by_job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (source_project_id <> target_project_id),
  UNIQUE (source_project_id, target_project_id, link_type)
);

CREATE INDEX IF NOT EXISTS project_links_repository_id_idx
  ON project_links (repository_id);

UPDATE system_metadata SET value = 'stage-04', updated_at = now()
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS project_links_repository_id_idx;
DROP TABLE IF EXISTS project_links;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_default_workspace_id_fk;
DROP INDEX IF EXISTS workspaces_environment_id_idx;
DROP INDEX IF EXISTS workspaces_project_id_idx;
DROP TABLE IF EXISTS workspaces;
DROP INDEX IF EXISTS projects_status_updated_at_idx;
DROP INDEX IF EXISTS projects_default_workspace_id_idx;
DROP INDEX IF EXISTS projects_environment_id_idx;
DROP INDEX IF EXISTS projects_repository_id_idx;
DROP INDEX IF EXISTS projects_root_path_status_idx;
DROP TABLE IF EXISTS projects;
DROP INDEX IF EXISTS repositories_superseded_by_idx;
DROP INDEX IF EXISTS repositories_discovery_source_idx;
DROP INDEX IF EXISTS repositories_status_idx;
DROP INDEX IF EXISTS repositories_local_path_idx;
DROP INDEX IF EXISTS repositories_provider_instance_full_path_idx;
DROP INDEX IF EXISTS repositories_provider_host_full_path_idx;
DROP INDEX IF EXISTS repositories_generic_local_identity_uidx;
DROP INDEX IF EXISTS repositories_provider_identity_uidx;
DROP TABLE IF EXISTS repositories;
DROP TABLE IF EXISTS environments;
DROP INDEX IF EXISTS root_paths_enabled_path_idx;
DROP TABLE IF EXISTS root_paths;
DROP INDEX IF EXISTS ignore_rules_scope_order_idx;
ALTER TABLE ignore_rules DROP COLUMN IF EXISTS sort_order;

UPDATE system_metadata SET value = 'stage-03', updated_at = now()
  WHERE key = 'schema_version';
