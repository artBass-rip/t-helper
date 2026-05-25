-- +goose Up
ALTER TABLE root_paths ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('config', 'api'));

CREATE INDEX IF NOT EXISTS root_paths_source_enabled_idx
  ON root_paths (source, enabled);

UPDATE system_metadata SET value = 'stage-04', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS root_paths_source_enabled_idx;

CREATE TABLE root_paths_old (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  schedule_enabled INTEGER NOT NULL DEFAULT 0 CHECK (schedule_enabled IN (0, 1)),
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT INTO root_paths_old (id, name, path, enabled, schedule_enabled, schedule_frequency, created_at, updated_at)
  SELECT id, name, path, enabled, schedule_enabled, schedule_frequency, created_at, updated_at FROM root_paths;

DROP TABLE root_paths;
ALTER TABLE root_paths_old RENAME TO root_paths;

CREATE INDEX IF NOT EXISTS root_paths_enabled_path_idx
  ON root_paths (enabled, path);

UPDATE system_metadata SET value = 'stage-04', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';
