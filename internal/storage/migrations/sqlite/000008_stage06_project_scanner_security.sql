-- +goose Up
CREATE TABLE IF NOT EXISTS tool_profiles (
  id TEXT PRIMARY KEY,
  tool TEXT NOT NULL CHECK (tool IN ('terraform', 'tflint', 'trivy', 'checkov', 'gitleaks', 'opa', 'conftest')),
  profile_id TEXT NOT NULL,
  profile_version TEXT NOT NULL,
  schema_version TEXT NOT NULL,
  source_type TEXT NOT NULL CHECK (source_type IN ('bundled', 'local_upload', 'local_path', 'generated_candidate')),
  source_path TEXT,
  checksum TEXT,
  certified_versions TEXT NOT NULL DEFAULT '[]',
  compatible_versions TEXT NOT NULL DEFAULT '[]',
  active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (tool, profile_id, profile_version)
);

CREATE INDEX IF NOT EXISTS tool_profiles_tool_active_idx
  ON tool_profiles (tool, active);

CREATE TABLE IF NOT EXISTS tool_profile_validation_results (
  id TEXT PRIMARY KEY,
  tool_profile_id TEXT,
  tool TEXT NOT NULL,
  tool_version TEXT,
  fixture_set TEXT,
  validation_status TEXT NOT NULL CHECK (validation_status IN ('passed', 'failed', 'warning')),
  diagnostics TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY (tool_profile_id) REFERENCES tool_profiles(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS project_scan_settings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL UNIQUE,
  scan_enabled INTEGER NOT NULL DEFAULT 1 CHECK (scan_enabled IN (0, 1)),
  schedule_enabled INTEGER NOT NULL DEFAULT 0 CHECK (schedule_enabled IN (0, 1)),
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  run_after_clone INTEGER NOT NULL DEFAULT 0 CHECK (run_after_clone IN (0, 1)),
  run_after_pull INTEGER NOT NULL DEFAULT 0 CHECK (run_after_pull IN (0, 1)),
  scan_type TEXT NOT NULL DEFAULT 'terraform_full' CHECK (scan_type IN ('terraform_static', 'terraform_validate', 'terraform_security', 'terraform_full', 'security_validation')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS project_security_scan_settings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  enabled_modules TEXT NOT NULL DEFAULT '["trivy"]',
  schedule_enabled INTEGER NOT NULL DEFAULT 0 CHECK (schedule_enabled IN (0, 1)),
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  validate_code INTEGER NOT NULL DEFAULT 1 CHECK (validate_code IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS security_rule_sets (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  source_type TEXT NOT NULL CHECK (source_type IN ('bundled', 'local_upload', 'local_path')),
  checksum TEXT,
  active INTEGER NOT NULL DEFAULT 0 CHECK (active IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (name, version)
);

CREATE INDEX IF NOT EXISTS security_rule_sets_active_idx
  ON security_rule_sets (active, name, version);

CREATE TABLE IF NOT EXISTS project_scans (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL UNIQUE,
  project_id TEXT NOT NULL,
  rule_set_id TEXT,
  scan_type TEXT NOT NULL CHECK (scan_type IN ('terraform_static', 'terraform_validate', 'terraform_security', 'terraform_full', 'security_validation')),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'partial', 'cancelled')),
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  result_payload TEXT,
  error_message TEXT,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE RESTRICT,
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT,
  FOREIGN KEY (rule_set_id) REFERENCES security_rule_sets(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS project_scans_project_status_idx
  ON project_scans (project_id, status);
CREATE INDEX IF NOT EXISTS project_scans_updated_at_idx
  ON project_scans (updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS security_findings (
  id TEXT PRIMARY KEY,
  project_id TEXT,
  repository_id TEXT,
  workspace_id TEXT,
  job_id TEXT,
  rule_set_id TEXT,
  check_type TEXT NOT NULL,
  rule_id TEXT NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical')),
  status TEXT NOT NULL CHECK (status IN ('open', 'accepted', 'false_positive', 'fixed', 'suppressed')),
  file_path TEXT,
  resource_ref TEXT,
  title TEXT NOT NULL,
  description TEXT,
  remediation TEXT,
  fingerprint TEXT NOT NULL UNIQUE,
  fingerprint_schema_version TEXT NOT NULL,
  fingerprint_components TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  detected_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (project_id IS NOT NULL OR repository_id IS NOT NULL OR workspace_id IS NOT NULL OR job_id IS NOT NULL),
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL,
  FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE SET NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL,
  FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE SET NULL,
  FOREIGN KEY (rule_set_id) REFERENCES security_rule_sets(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS security_findings_rule_status_idx
  ON security_findings (rule_set_id, status);
CREATE INDEX IF NOT EXISTS security_findings_project_status_idx
  ON security_findings (project_id, status);
CREATE INDEX IF NOT EXISTS security_findings_repository_status_idx
  ON security_findings (repository_id, status);
CREATE INDEX IF NOT EXISTS security_findings_job_idx
  ON security_findings (job_id);
CREATE INDEX IF NOT EXISTS security_findings_severity_status_idx
  ON security_findings (severity, status);
CREATE INDEX IF NOT EXISTS security_findings_updated_at_idx
  ON security_findings (updated_at DESC, id DESC);

INSERT INTO tool_profiles (id, tool, profile_id, profile_version, schema_version, source_type, checksum, certified_versions, compatible_versions, active, created_at, updated_at)
VALUES
  ('tool_profile_terraform_validate_v1', 'terraform', 'terraform-validate-json-v1', '1.0.0', 'tool_profile.v1', 'bundled', 'builtin:terraform-validate-json-v1', '["1"]', '["1"]', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ('tool_profile_tflint_v1', 'tflint', 'tflint-json-v1', '1.0.0', 'tool_profile.v1', 'bundled', 'builtin:tflint-json-v1', '["0"]', '["0"]', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ('tool_profile_trivy_v1', 'trivy', 'trivy-terraform-misconfig-json-v1', '1.0.0', 'tool_profile.v1', 'bundled', 'builtin:trivy-terraform-misconfig-json-v1', '["0"]', '["0"]', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT (tool, profile_id, profile_version) DO NOTHING;

INSERT INTO security_rule_sets (id, name, version, source_type, checksum, active, created_at, updated_at)
VALUES ('security_rule_set_mvp', 'MVP bundled Terraform security rules', '1.0.0', 'bundled', 'builtin:mvp', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT (name, version) DO NOTHING;

UPDATE system_metadata SET value = 'stage-06', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';

-- +goose Down
DROP INDEX IF EXISTS security_findings_updated_at_idx;
DROP INDEX IF EXISTS security_findings_severity_status_idx;
DROP INDEX IF EXISTS security_findings_job_idx;
DROP INDEX IF EXISTS security_findings_repository_status_idx;
DROP INDEX IF EXISTS security_findings_project_status_idx;
DROP INDEX IF EXISTS security_findings_rule_status_idx;
DROP TABLE IF EXISTS security_findings;
DROP INDEX IF EXISTS project_scans_updated_at_idx;
DROP INDEX IF EXISTS project_scans_project_status_idx;
DROP TABLE IF EXISTS project_scans;
DROP INDEX IF EXISTS security_rule_sets_active_idx;
DROP TABLE IF EXISTS security_rule_sets;
DROP TABLE IF EXISTS project_security_scan_settings;
DROP TABLE IF EXISTS project_scan_settings;
DROP TABLE IF EXISTS tool_profile_validation_results;
DROP INDEX IF EXISTS tool_profiles_tool_active_idx;
DROP TABLE IF EXISTS tool_profiles;

UPDATE system_metadata SET value = 'stage-05', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE key = 'schema_version';
