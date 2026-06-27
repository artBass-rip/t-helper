-- +goose Up
CREATE TABLE IF NOT EXISTS project_scan_findings (
  project_scan_id TEXT NOT NULL REFERENCES project_scans(id) ON DELETE CASCADE,
  finding_id TEXT NOT NULL REFERENCES security_findings(id) ON DELETE CASCADE,
  detected_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (project_scan_id, finding_id)
);

CREATE INDEX IF NOT EXISTS project_scan_findings_finding_idx
  ON project_scan_findings (finding_id, project_scan_id);

UPDATE tool_profiles SET certified_versions = '[">=1.15.0"]'::jsonb, compatible_versions = '[">=1.15.0"]'::jsonb, updated_at = now()
  WHERE tool = 'terraform' AND profile_id = 'terraform-validate-json-v1' AND profile_version = '1.0.0';
UPDATE tool_profiles SET certified_versions = '["0.63.1"]'::jsonb, compatible_versions = '["0"]'::jsonb, updated_at = now()
  WHERE tool = 'tflint' AND profile_id = 'tflint-json-v1' AND profile_version = '1.0.0';
UPDATE tool_profiles SET certified_versions = '["0.71.2"]'::jsonb, compatible_versions = '["0"]'::jsonb, updated_at = now()
  WHERE tool = 'trivy' AND profile_id = 'trivy-terraform-misconfig-json-v1' AND profile_version = '1.0.0';

-- +goose Down
UPDATE tool_profiles SET certified_versions = '["1"]'::jsonb, compatible_versions = '["1"]'::jsonb, updated_at = now()
  WHERE tool = 'terraform' AND profile_id = 'terraform-validate-json-v1' AND profile_version = '1.0.0';
UPDATE tool_profiles SET certified_versions = '["0"]'::jsonb, compatible_versions = '["0"]'::jsonb, updated_at = now()
  WHERE tool IN ('tflint', 'trivy');
DROP INDEX IF EXISTS project_scan_findings_finding_idx;
DROP TABLE IF EXISTS project_scan_findings;
