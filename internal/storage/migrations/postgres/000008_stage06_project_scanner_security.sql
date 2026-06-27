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
  certified_versions JSONB NOT NULL DEFAULT '[]'::jsonb,
  compatible_versions JSONB NOT NULL DEFAULT '[]'::jsonb,
  active BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (tool, profile_id, profile_version)
);

CREATE INDEX IF NOT EXISTS tool_profiles_tool_active_idx
  ON tool_profiles (tool, active);

CREATE TABLE IF NOT EXISTS tool_profile_validation_results (
  id TEXT PRIMARY KEY,
  tool_profile_id TEXT REFERENCES tool_profiles(id) ON DELETE SET NULL,
  tool TEXT NOT NULL,
  tool_version TEXT,
  fixture_set TEXT,
  validation_status TEXT NOT NULL CHECK (validation_status IN ('passed', 'failed', 'warning')),
  diagnostics JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS tool_profile_validation_results_profile_version_idx
  ON tool_profile_validation_results (tool_profile_id, tool_version);
CREATE INDEX IF NOT EXISTS tool_profile_validation_results_status_idx
  ON tool_profile_validation_results (validation_status);

CREATE TABLE IF NOT EXISTS project_scan_settings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
  scan_enabled BOOLEAN NOT NULL DEFAULT true,
  schedule_enabled BOOLEAN NOT NULL DEFAULT false,
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  run_after_clone BOOLEAN NOT NULL DEFAULT false,
  run_after_pull BOOLEAN NOT NULL DEFAULT false,
  scan_type TEXT NOT NULL DEFAULT 'terraform_full' CHECK (scan_type IN ('terraform_static', 'terraform_validate', 'terraform_security', 'terraform_full', 'security_validation')),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS project_security_scan_settings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
  enabled BOOLEAN NOT NULL DEFAULT true,
  enabled_modules JSONB NOT NULL DEFAULT '["trivy"]'::jsonb,
  schedule_enabled BOOLEAN NOT NULL DEFAULT false,
  schedule_frequency TEXT CHECK (schedule_frequency IS NULL OR schedule_frequency IN ('daily', 'weekly', 'monthly')),
  validate_code BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS security_rule_sets (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  version TEXT NOT NULL,
  source_type TEXT NOT NULL CHECK (source_type IN ('bundled', 'local_upload', 'local_path')),
  checksum TEXT,
  active BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (name, version)
);

CREATE INDEX IF NOT EXISTS security_rule_sets_active_idx
  ON security_rule_sets (active, name, version);

CREATE TABLE IF NOT EXISTS project_scans (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE RESTRICT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  rule_set_id TEXT REFERENCES security_rule_sets(id) ON DELETE SET NULL,
  scan_type TEXT NOT NULL CHECK (scan_type IN ('terraform_static', 'terraform_validate', 'terraform_security', 'terraform_full', 'security_validation')),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'partial', 'cancelled')),
  created_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  result_payload JSONB,
  error_message TEXT,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS project_scans_project_status_idx
  ON project_scans (project_id, status);
CREATE INDEX IF NOT EXISTS project_scans_updated_at_idx
  ON project_scans (updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS security_findings (
  id TEXT PRIMARY KEY,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
  repository_id TEXT REFERENCES repositories(id) ON DELETE SET NULL,
  workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
  job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
  rule_set_id TEXT REFERENCES security_rule_sets(id) ON DELETE SET NULL,
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
  fingerprint_components JSONB NOT NULL,
  first_seen_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  detected_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (project_id IS NOT NULL OR repository_id IS NOT NULL OR workspace_id IS NOT NULL OR job_id IS NOT NULL)
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
  ('tool_profile_terraform_validate_v1', 'terraform', 'terraform-validate-json-v1', '1.0.0', 'tool_profile.v1', 'bundled', 'builtin:terraform-validate-json-v1', '[">=1.15.0"]'::jsonb, '[">=1.15.0"]'::jsonb, true, now(), now()),
  ('tool_profile_tflint_v1', 'tflint', 'tflint-json-v1', '1.0.0', 'tool_profile.v1', 'bundled', 'builtin:tflint-json-v1', '["0.63.1"]'::jsonb, '["0"]'::jsonb, true, now(), now()),
  ('tool_profile_trivy_v1', 'trivy', 'trivy-terraform-misconfig-json-v1', '1.0.0', 'tool_profile.v1', 'bundled', 'builtin:trivy-terraform-misconfig-json-v1', '["0.71.2"]'::jsonb, '["0"]'::jsonb, true, now(), now())
ON CONFLICT (tool, profile_id, profile_version) DO NOTHING;

INSERT INTO security_rule_sets (id, name, version, source_type, checksum, active, created_at, updated_at)
VALUES ('security_rule_set_mvp', 'MVP bundled Terraform security rules', '1.0.0', 'bundled', 'builtin:mvp', true, now(), now())
ON CONFLICT (name, version) DO NOTHING;

UPDATE system_metadata SET value = 'stage-06', updated_at = now()
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
DROP INDEX IF EXISTS tool_profile_validation_results_status_idx;
DROP INDEX IF EXISTS tool_profile_validation_results_profile_version_idx;
DROP TABLE IF EXISTS tool_profile_validation_results;
DROP INDEX IF EXISTS tool_profiles_tool_active_idx;
DROP TABLE IF EXISTS tool_profiles;

UPDATE system_metadata SET value = 'stage-05', updated_at = now()
  WHERE key = 'schema_version';
