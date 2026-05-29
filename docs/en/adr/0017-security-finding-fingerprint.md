# ADR 0017: Security finding fingerprint

## Status

Accepted.

## Decision

Security findings use a versioned deterministic fingerprint:

```text
fingerprint = fp:v1:<hex sha256(canonical_json(fingerprint_components))>
```

`fingerprint_components` uses canonical JSON:

- UTF-8;
- sorted object keys;
- no insignificant whitespace;
- stable string normalization;
- explicit `null` for nullable identity fields.

Required components:

```json
{
  "schema_version": "security_finding.fingerprint.v1",
  "project_id": "project_opaque_id",
  "workspace_id": null,
  "rule_set_id": "rule_set_opaque_id",
  "rule_namespace": "trivy",
  "tool": "trivy",
  "check_type": "terraform.security.misconfig",
  "rule_id": "AVD-AWS-0088",
  "normalized_file_path": "modules/s3/main.tf",
  "resource_ref": "aws_s3_bucket.example",
  "finding_key": "aws_s3_bucket.example:public-read"
}
```

Fingerprint components must not include:

- `job_id`;
- `project_scan_id`;
- line or column;
- title;
- description;
- remediation;
- severity;
- raw Terraform source;
- secrets or secret values.

`resource_ref` or `finding_key` is required for persisted `security_findings`. If a tool cannot provide a stable resource reference, the adapter must derive a stable `finding_key` from normalized tool output without including secret material.

`normalized_file_path` rules:

- relative to Terraform project root;
- `/` separator;
- no leading slash;
- no `.` or `..` path segments;
- no symlink-resolved absolute path;
- case-sensitive by default.

`rule_namespace` identifies the rule source, for example:

```text
trivy
checkov
tflint
gitleaks
opa:<policy_pack_id>
conftest:<policy_pack_id>
```

## Examples

Line shift:

- scan A reports `modules/s3/main.tf:12`;
- scan B reports the same resource/rule at `modules/s3/main.tf:19`;
- fingerprint remains the same because line and column are excluded.

Renamed file:

- `normalized_file_path = modules/s3/main.tf` changes to `modules/storage/main.tf`;
- fingerprint changes unless the adapter has a stable tool-specific `finding_key`
  that intentionally survives file moves and the rule contract documents that behavior.

Changed rule set:

- same tool, rule and resource under `rule_set_id = baseline-1`;
- later scan uses `rule_set_id = baseline-2`;
- fingerprint changes because different rule sets can intentionally define different policy semantics.

Workspace-specific finding:

- same project/rule/resource in `workspace_id = dev` and `workspace_id = prod`;
- fingerprints differ because workspace is part of the identity.

Finding without `resource_ref`:

- tool output does not provide a stable Terraform resource address;
- adapter derives `finding_key` from normalized non-secret tool fields, for example
  `file:modules/iam/main.tf|rule:CKV_AWS_1|block:aws_iam_policy_document`;
- persisted finding is allowed only if `finding_key` is stable and contains no secret material.

## Rationale

Findings must be deduplicated across repeated scans without merging unrelated issues. Job ids, scan ids, line numbers, titles and severity can change between runs and must not define identity.

Including project, workspace, rule set, rule namespace, tool, rule id, normalized file path and stable resource/finding key gives deterministic finding identity while preserving separation between different policy packs and projects.

## Consequences

- Stage 06 stores `fingerprint_schema_version`, `fingerprint_components`, `first_seen_at` and `last_seen_at`.
- `security_findings.fingerprint` is unique.
- Repeat scans update existing finding rows instead of creating duplicates when fingerprint matches.
- Finding status lifecycle can distinguish new, still-open and no-longer-seen findings through `first_seen_at`, `last_seen_at` and status.
- Tests must verify stable fingerprints across repeated scans and changed job ids, changed scan ids and line shifts.
- Tests must cover renamed file, changed rule set, workspace-specific findings and findings that rely on `finding_key` without `resource_ref`.
