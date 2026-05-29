# ADR 0018: External toolchain profiles

## Status

Accepted.

## Decision

Stage 06 scanner integrations use profile-based external tool compatibility.
`project-scanner` and `security-validator` do not parse raw CLI output directly.
They call a shared tool adapter runtime:

```text
tool runner
  -> version discovery
  -> tool profile selection
  -> command execution
  -> output parser
  -> normalization mapper
  -> internal scan DTO / findings
```

The stable contract inside `t-helper` is the normalized scan DTO and persisted
entities. CLI command shapes, supported version ranges, output parsing and field
mapping are described by versioned tool profile files.

Bundled profile files live under:

```text
tool-profiles/
  terraform/
  tflint/
  trivy/
  checkov/
```

MVP profiles are required for:

- `terraform validate`;
- `TFLint`;
- `Trivy` as the mandatory Stage 06 MVP security scanner.

Tool profiles are declarative and may use only approved parser primitives:

- JSON with JSONPath or JMESPath selectors;
- regex named groups;
- line parsers;
- enum and severity mapping tables;
- default values;
- required field validation;
- redaction rules.

Tool profiles must not contain:

- shell fragments;
- arbitrary scripts or eval behavior;
- network calls;
- file reads outside the captured tool output;
- commands other than explicit version discovery and scan command templates.

Profile fields include:

- `schema_version`;
- `tool`;
- `profile_id`;
- `profile_version`;
- `certified_versions`;
- `compatible_versions`;
- `version_discovery`;
- `scan_command`;
- `exit_codes`;
- `parser`;
- `mapping`;
- `fingerprint`;
- `redaction`.

Runtime version policy supports:

```text
certified_only
compatible_range
latest_best_effort
```

Default MVP policy is `certified_only`. In `compatible_range`, supported but
uncertified versions may run and must be marked as `compatible` /
`uncertified` in result metadata. In `latest_best_effort`, unknown release
versions may run only with explicit opt-in and must be marked as `uncertified`.

Machine-readable errors:

```text
tool_not_found
tool_version_unsupported
tool_version_uncertified
tool_output_parse_failed
tool_schema_unsupported
tool_profile_not_found
tool_profile_validation_failed
```

Raw CLI output is not the primary persistence contract. `jobs.result_payload`,
`project_scans.result_payload` and `security_findings` store normalized metadata
and must not contain raw Terraform source, secrets, private keys, tokens or
credential-bearing URLs. Diagnostic snippets may be stored only after redaction,
size limits and explicit schema fields.

## Profile analyzer

`t-helper` may include an optional `tool-profile-analyzer` used by maintainers
or operators to prepare new profile versions without changing `t-helper` code.

The analyzer consumes:

- raw sample stdout/stderr;
- command exit code;
- discovered tool version;
- expected normalized DTO fixtures when available;
- an existing profile version as a baseline when available.

The analyzer may generate candidate profile files, parser selectors, mapping
tables and validation fixtures. Generated profiles are never activated
automatically. They must pass profile validation and be imported/activated
explicitly through CLI/API before runtime uses them.

Analyzer output must include confidence and unresolved mapping diagnostics. Any
field that affects fingerprints (`rule_id`, normalized file path,
`resource_ref`, `finding_key`, `rule_namespace`, `tool`, `check_type`) requires
explicit validation against fixtures before activation.

## Migration strategy

Fresh tool versions are adopted without changing `t-helper` code when their
output can be represented by existing parser primitives:

1. Capture new tool outputs on approved fixtures.
2. Run profile validation against the existing profile.
3. If compatible, extend `compatible_versions` or `certified_versions`.
4. If output changed, create a new profile version.
5. Validate expected normalized DTOs, exit code behavior, redaction and
   fingerprint components.
6. Import and explicitly activate the new profile.

Acceptance tests use certified profile versions. Latest-version testing may run
as a non-blocking compatibility suite, but does not define MVP acceptance.

## Rationale

External CLI tools do not provide a stable internal API for `t-helper`. JSON
schemas, exit codes, rule identifiers, severity names and diagnostic fields can
change between releases. Hard-coding parser logic in scanners would make Stage
06 acceptance unstable and require `t-helper` code changes for routine tool
upgrades.

Profile-based compatibility keeps the scanner code stable while allowing
operators to certify new external tool versions through data files and fixtures.

## Consequences

- Stage 06 implements a shared tool runner, profile registry, profile validator
  and normalized DTO mapper.
- Bundled profiles are required for the Stage 06 MVP toolchain.
- New tool versions can be adopted by adding or updating profile files when no
  new parser primitive is needed.
- If a tool output change cannot be represented by supported profile primitives,
  a code change or a new adapter capability is required.
- Tests must cover certified, compatible and unsupported versions, profile
  validation, redaction and fingerprint stability.
