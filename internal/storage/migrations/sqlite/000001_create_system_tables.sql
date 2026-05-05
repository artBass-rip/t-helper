-- +goose Up
CREATE TABLE IF NOT EXISTS system_metadata (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT OR IGNORE INTO system_metadata (key, value) VALUES
  ('schema_version', 'stage-01'),
  ('schema_owner', 'stage-01-backend-storage-foundation'),
  ('health_schema_version', 'health_status.v1');

-- +goose Down
DROP TABLE IF EXISTS system_metadata;
