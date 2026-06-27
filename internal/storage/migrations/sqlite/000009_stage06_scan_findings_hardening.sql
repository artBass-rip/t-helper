-- +goose Up
CREATE TABLE IF NOT EXISTS project_scan_findings (
  project_scan_id TEXT NOT NULL,
  finding_id TEXT NOT NULL,
  detected_at TEXT NOT NULL,
  PRIMARY KEY (project_scan_id, finding_id),
  FOREIGN KEY (project_scan_id) REFERENCES project_scans(id) ON DELETE CASCADE,
  FOREIGN KEY (finding_id) REFERENCES security_findings(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS project_scan_findings_finding_idx
  ON project_scan_findings (finding_id, project_scan_id);

UPDATE tool_profiles SET certified_versions = '[">=1.15.0"]', compatible_versions = '[">=1.15.0"]', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE tool = 'terraform' AND profile_id = 'terraform-validate-json-v1' AND profile_version = '1.0.0';
UPDATE tool_profiles SET certified_versions = '["0.63.1"]', compatible_versions = '["0"]', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE tool = 'tflint' AND profile_id = 'tflint-json-v1' AND profile_version = '1.0.0';
UPDATE tool_profiles SET certified_versions = '["0.71.2"]', compatible_versions = '["0"]', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE tool = 'trivy' AND profile_id = 'trivy-terraform-misconfig-json-v1' AND profile_version = '1.0.0';

-- +goose Down
UPDATE tool_profiles SET certified_versions = '["1"]', compatible_versions = '["1"]', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE tool = 'terraform' AND profile_id = 'terraform-validate-json-v1' AND profile_version = '1.0.0';
UPDATE tool_profiles SET certified_versions = '["0"]', compatible_versions = '["0"]', updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE tool IN ('tflint', 'trivy');
DROP INDEX IF EXISTS project_scan_findings_finding_idx;
DROP TABLE IF EXISTS project_scan_findings;
