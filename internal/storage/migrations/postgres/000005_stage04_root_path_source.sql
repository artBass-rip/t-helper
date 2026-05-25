-- +goose Up
ALTER TABLE root_paths ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('config', 'api'));

CREATE INDEX IF NOT EXISTS root_paths_source_enabled_idx
  ON root_paths (source, enabled);

UPDATE system_metadata SET value = 'stage-04', updated_at = now()
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS root_paths_source_enabled_idx;
ALTER TABLE root_paths DROP COLUMN IF EXISTS source;

UPDATE system_metadata SET value = 'stage-04', updated_at = now()
  WHERE key = 'schema_version';
