-- +goose Up
CREATE TABLE IF NOT EXISTS root_paths (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  schedule_enabled INTEGER NOT NULL DEFAULT 0 CHECK (schedule_enabled IN (0, 1)),
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS root_paths_enabled_path_idx
  ON root_paths (enabled, path);

CREATE TABLE IF NOT EXISTS environments (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  code TEXT NOT NULL UNIQUE,
  description TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
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
  UNIQUE (provider, provider_host, full_path),
  FOREIGN KEY (root_path_id) REFERENCES root_paths(id) ON DELETE RESTRICT,
  FOREIGN KEY (superseded_by_repository_id) REFERENCES repositories(id) ON DELETE SET NULL
);

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
  root_path_id TEXT NOT NULL,
  terraform_marker TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'disabled')),
  repository_id TEXT,
  environment_id TEXT,
  default_workspace_id TEXT,
  detected_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (root_path_id, relative_path),
  FOREIGN KEY (root_path_id) REFERENCES root_paths(id) ON DELETE RESTRICT,
  FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE SET NULL,
  FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE SET NULL,
  FOREIGN KEY (default_workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL
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
  project_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  name TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (project_id, name),
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS workspaces_project_id_idx
  ON workspaces (project_id);
CREATE INDEX IF NOT EXISTS workspaces_environment_id_idx
  ON workspaces (environment_id);

CREATE TABLE IF NOT EXISTS project_links (
  id TEXT PRIMARY KEY,
  source_project_id TEXT NOT NULL,
  target_project_id TEXT NOT NULL,
  link_type TEXT NOT NULL CHECK (link_type IN ('same_repository')),
  repository_id TEXT,
  detected_by_job_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (source_project_id <> target_project_id),
  UNIQUE (source_project_id, target_project_id, link_type),
  FOREIGN KEY (source_project_id) REFERENCES projects(id) ON DELETE CASCADE,
  FOREIGN KEY (target_project_id) REFERENCES projects(id) ON DELETE CASCADE,
  FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE SET NULL,
  FOREIGN KEY (detected_by_job_id) REFERENCES jobs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS project_links_repository_id_idx
  ON project_links (repository_id);

UPDATE system_metadata SET value = 'stage-04', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS project_links_repository_id_idx;
DROP TABLE IF EXISTS project_links;
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
DROP TABLE IF EXISTS repositories;
DROP TABLE IF EXISTS environments;
DROP INDEX IF EXISTS root_paths_enabled_path_idx;
DROP TABLE IF EXISTS root_paths;

UPDATE system_metadata SET value = 'stage-03', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';
