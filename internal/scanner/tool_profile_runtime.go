package scanner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	toolProfileSchemaVersion    = "tool_profile.v1"
	toolProfileCandidateVersion = "tool_profile_candidate.v1"
	defaultVersionPolicy        = "certified_only"
)

type toolProfileDocument struct {
	SchemaVersion      string                         `json:"schema_version"`
	Tool               string                         `json:"tool"`
	ProfileID          string                         `json:"profile_id"`
	ProfileVersion     string                         `json:"profile_version"`
	CertifiedVersions  []string                       `json:"certified_versions"`
	CompatibleVersions []string                       `json:"compatible_versions"`
	VersionPolicy      string                         `json:"version_policy"`
	VersionDiscovery   toolProfileCommand             `json:"version_discovery"`
	ScanCommand        toolProfileCommand             `json:"scan_command"`
	Parser             toolProfileParser              `json:"parser"`
	Mapping            map[string]toolProfileSelector `json:"mapping"`
	Redaction          toolProfileRedaction           `json:"redaction"`
}

type toolProfileCommand struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type toolProfileParser struct {
	Type       string                         `json:"type"`
	Result     string                         `json:"result,omitempty"`
	Findings   toolProfileFindingParser       `json:"findings,omitempty"`
	Severities map[string]string              `json:"severity_map,omitempty"`
	Defaults   map[string]toolProfileSelector `json:"defaults,omitempty"`
}

type toolProfileFindingParser struct {
	ResultsPath string                         `json:"results_path"`
	ItemsPath   string                         `json:"items_path"`
	Mapping     map[string]toolProfileSelector `json:"mapping"`
}

type toolProfileSelector struct {
	Path     string   `json:"path,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Value    any      `json:"value,omitempty"`
	Join     string   `json:"join,omitempty"`
	Template string   `json:"template,omitempty"`
}

type toolProfileRedaction struct {
	MaxOutputBytes int `json:"max_output_bytes"`
}

type toolProfileRunResult struct {
	Tool       ToolMetadata
	Check      map[string]any
	Findings   []FindingUpsert
	RawSummary map[string]any
}

func runToolProfile(ctx context.Context, store *Store, tool, projectDir string) (toolProfileRunResult, error) {
	doc, profile, err := store.activeToolProfileDocument(ctx, tool)
	if err != nil {
		return toolProfileRunResult{Tool: ToolMetadata{Tool: tool, CompatibilityStatus: "unsupported", CertificationStatus: "uncertified"}}, err
	}
	version, err := discoverProfileToolVersion(ctx, doc)
	if err != nil {
		return toolProfileRunResult{Tool: ToolMetadata{Tool: tool, ProfileID: profile.ProfileID, ProfileVersion: profile.ProfileVersion, CompatibilityStatus: "unknown", CertificationStatus: "uncertified"}}, err
	}
	meta, err := profileToolMetadata(tool, profile, doc, version)
	if err != nil {
		return toolProfileRunResult{Tool: meta}, err
	}
	cmd := exec.CommandContext(ctx, doc.ScanCommand.Command, renderCommandArgs(doc.ScanCommand.Args, projectDir)...)
	cmd.Dir = projectDir
	output, runErr := cmd.Output()
	if len(output) > doc.redactionLimit() {
		output = output[:doc.redactionLimit()]
	}
	var parsed any
	if len(output) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(output))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			return toolProfileRunResult{Tool: meta}, toolRunError{code: "tool_output_parse_failed", message: tool + " output could not be parsed by active profile", retryable: false}
		}
	}
	if runErr != nil && len(output) == 0 {
		return toolProfileRunResult{Tool: meta}, toolRunError{code: "tool_failed", message: tool + " scan failed", retryable: false}
	}
	result := toolProfileRunResult{Tool: meta}
	switch doc.Parser.Result {
	case "terraform_validate":
		result.Check = map[string]any{
			"tool":           tool,
			"check_type":     "terraform.validate",
			"valid":          selectorBool(parsed, doc.Mapping["valid"]),
			"errors_count":   selectorInt(parsed, doc.Mapping["errors_count"]),
			"warnings_count": selectorInt(parsed, doc.Mapping["warnings_count"]),
		}
	case "tflint":
		result.Check = map[string]any{
			"tool":         tool,
			"check_type":   "terraform.lint",
			"issues_count": len(selectorArray(parsed, doc.Mapping["issues"])),
		}
	case "findings":
		result.Findings = profileFindings(parsed, doc, projectDir)
	default:
		return result, toolRunError{code: "tool_profile_validation_failed", message: "unsupported profile parser result " + doc.Parser.Result, retryable: false}
	}
	return result, nil
}

func (s *Store) activeToolProfileDocument(ctx context.Context, tool string) (toolProfileDocument, ToolProfile, error) {
	profile, err := s.ActiveToolProfile(ctx, tool)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return toolProfileDocument{}, ToolProfile{}, toolRunError{code: "tool_profile_not_found", message: "active tool profile not found for " + tool, retryable: false}
		}
		return toolProfileDocument{}, ToolProfile{}, err
	}
	doc, err := loadToolProfileDocument(profile)
	if err != nil {
		return toolProfileDocument{}, profile, err
	}
	return doc, profile, nil
}

func loadToolProfileDocument(profile ToolProfile) (toolProfileDocument, error) {
	path := strings.TrimSpace(profile.SourcePath)
	if path == "" && profile.SourceType == "bundled" {
		path = filepath.Join("tool-profiles", profile.Tool, profile.ProfileID+".json")
	}
	if path == "" {
		return toolProfileDocument{}, validationErrorf("tool profile source_path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil && !filepath.IsAbs(path) {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			if candidate := findRelativeFileUpwards(cwd, path); candidate != "" {
				raw, err = os.ReadFile(candidate)
			}
		}
	}
	if err != nil {
		return toolProfileDocument{}, toolRunError{code: "tool_profile_not_found", message: "tool profile file not found: " + path, retryable: false}
	}
	doc, err := decodeToolProfile(raw)
	if err != nil {
		return toolProfileDocument{}, err
	}
	if doc.Tool != profile.Tool || doc.ProfileID != profile.ProfileID || doc.ProfileVersion != profile.ProfileVersion {
		return toolProfileDocument{}, toolRunError{code: "tool_profile_validation_failed", message: "tool profile file does not match active metadata", retryable: false}
	}
	return doc, nil
}

func findRelativeFileUpwards(startDir, rel string) string {
	dir := startDir
	for {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func decodeToolProfile(raw []byte) (toolProfileDocument, error) {
	var doc toolProfileDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return doc, toolRunError{code: "tool_profile_validation_failed", message: err.Error(), retryable: false}
	}
	if doc.VersionPolicy == "" {
		doc.VersionPolicy = defaultVersionPolicy
	}
	if err := validateToolProfileDocument(doc); err != nil {
		return doc, err
	}
	return doc, nil
}

func validateToolProfileDocument(doc toolProfileDocument) error {
	if doc.SchemaVersion != toolProfileSchemaVersion {
		return toolRunError{code: "tool_schema_unsupported", message: "unsupported tool profile schema_version", retryable: false}
	}
	if doc.Tool == "" || doc.ProfileID == "" || doc.ProfileVersion == "" {
		return toolRunError{code: "tool_profile_validation_failed", message: "tool, profile_id and profile_version are required", retryable: false}
	}
	if !allowedTool(doc.Tool) {
		return toolRunError{code: "tool_profile_validation_failed", message: "unsupported tool " + doc.Tool, retryable: false}
	}
	if doc.VersionPolicy != "certified_only" && doc.VersionPolicy != "compatible_range" && doc.VersionPolicy != "latest_best_effort" {
		return toolRunError{code: "tool_profile_validation_failed", message: "unsupported version_policy", retryable: false}
	}
	if err := validateProfileCommand(doc.Tool, doc.VersionDiscovery); err != nil {
		return err
	}
	if err := validateProfileCommand(doc.Tool, doc.ScanCommand); err != nil {
		return err
	}
	if doc.Parser.Type != "json" {
		return toolRunError{code: "tool_profile_validation_failed", message: "only json parser is supported", retryable: false}
	}
	switch doc.Parser.Result {
	case "terraform_validate", "tflint", "findings":
	default:
		return toolRunError{code: "tool_profile_validation_failed", message: "unsupported parser result", retryable: false}
	}
	return nil
}

func validateProfileCommand(tool string, cmd toolProfileCommand) error {
	if cmd.Command != tool {
		return toolRunError{code: "tool_profile_validation_failed", message: "profile command must match tool", retryable: false}
	}
	for _, arg := range cmd.Args {
		if strings.ContainsAny(arg, "|;&`$<>") || strings.Contains(arg, "..") {
			return toolRunError{code: "tool_profile_validation_failed", message: "profile command contains unsupported shell-like syntax", retryable: false}
		}
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
			return toolRunError{code: "tool_profile_validation_failed", message: "profile command must not contain network URLs", retryable: false}
		}
		if filepath.IsAbs(arg) && arg != "{project_dir}" {
			return toolRunError{code: "tool_profile_validation_failed", message: "profile command must not read absolute paths", retryable: false}
		}
	}
	return nil
}

func allowedTool(tool string) bool {
	switch tool {
	case "terraform", "tflint", "trivy", "checkov", "gitleaks", "opa", "conftest":
		return true
	default:
		return false
	}
}

func discoverProfileToolVersion(ctx context.Context, doc toolProfileDocument) (string, error) {
	cmd := exec.CommandContext(ctx, doc.VersionDiscovery.Command, doc.VersionDiscovery.Args...)
	output, err := cmd.Output()
	if err != nil {
		return "", toolRunError{code: "tool_not_found", message: doc.Tool + " binary is unavailable", retryable: false}
	}
	fields := strings.Fields(string(output))
	for _, field := range fields {
		field = strings.TrimPrefix(field, "v")
		if len(field) > 0 && field[0] >= '0' && field[0] <= '9' {
			return field, nil
		}
	}
	return strings.TrimSpace(string(output)), nil
}

func profileToolMetadata(tool string, profile ToolProfile, doc toolProfileDocument, version string) (ToolMetadata, error) {
	meta := ToolMetadata{
		Tool:                tool,
		ToolVersion:         version,
		ProfileID:           profile.ProfileID,
		ProfileVersion:      profile.ProfileVersion,
		CompatibilityStatus: "compatible",
		CertificationStatus: "uncertified",
	}
	if stringSlicePrefixMatch(doc.CertifiedVersions, version) {
		meta.CompatibilityStatus = "certified"
		meta.CertificationStatus = "certified"
		return meta, nil
	}
	if doc.VersionPolicy == "certified_only" {
		meta.CompatibilityStatus = "unsupported"
		return meta, toolRunError{code: "tool_version_uncertified", message: fmt.Sprintf("%s version %s is not certified by the active profile", tool, version), retryable: false}
	}
	if stringSlicePrefixMatch(doc.CompatibleVersions, version) || doc.VersionPolicy == "latest_best_effort" {
		return meta, nil
	}
	meta.CompatibilityStatus = "unsupported"
	return meta, toolRunError{code: "tool_version_unsupported", message: fmt.Sprintf("%s version %s is not compatible with the active profile", tool, version), retryable: false}
}

func renderCommandArgs(args []string, projectDir string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, strings.ReplaceAll(arg, "{project_dir}", projectDir))
	}
	return out
}

func (doc toolProfileDocument) redactionLimit() int {
	if doc.Redaction.MaxOutputBytes <= 0 || doc.Redaction.MaxOutputBytes > 4*1024*1024 {
		return 4 * 1024 * 1024
	}
	return doc.Redaction.MaxOutputBytes
}

func stringSlicePrefixMatch(prefixes []string, version string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(version, prefix) {
			return true
		}
	}
	return false
}

func selectorValue(root any, selector toolProfileSelector) any {
	if selector.Value != nil {
		return selector.Value
	}
	if selector.Path != "" {
		return jsonPath(root, selector.Path)
	}
	if len(selector.Paths) > 0 {
		parts := make([]string, 0, len(selector.Paths))
		for _, path := range selector.Paths {
			if text := valueString(jsonPath(root, path)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, selector.Join)
	}
	return nil
}

func selectorString(root any, selector toolProfileSelector) string {
	if selector.Template != "" {
		return renderTemplate(selector.Template, root)
	}
	return valueString(selectorValue(root, selector))
}

func selectorBool(root any, selector toolProfileSelector) bool {
	value, _ := selectorValue(root, selector).(bool)
	return value
}

func selectorInt(root any, selector toolProfileSelector) int {
	switch value := selectorValue(root, selector).(type) {
	case json.Number:
		i, _ := value.Int64()
		return int(i)
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func selectorArray(root any, selector toolProfileSelector) []any {
	items, _ := selectorValue(root, selector).([]any)
	return items
}

func jsonPath(root any, path string) any {
	path = strings.TrimPrefix(strings.TrimSpace(path), "$.")
	if path == "" || path == "$" {
		return root
	}
	current := root
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil
		}
		if strings.HasSuffix(part, "[]") {
			part = strings.TrimSuffix(part, "[]")
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[part]
	}
	return current
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func renderTemplate(template string, root any) string {
	out := template
	for {
		start := strings.Index(out, "{{")
		end := strings.Index(out, "}}")
		if start < 0 || end < start {
			return out
		}
		key := strings.TrimSpace(out[start+2 : end])
		out = out[:start] + valueString(jsonPath(root, key)) + out[end+2:]
	}
}

func profileFindings(root any, doc toolProfileDocument, projectDir string) []FindingUpsert {
	results := selectorArray(root, toolProfileSelector{Path: doc.Parser.Findings.ResultsPath})
	var out []FindingUpsert
	for _, result := range results {
		items := selectorArray(result, toolProfileSelector{Path: doc.Parser.Findings.ItemsPath})
		for _, item := range items {
			merged := mergeObjects(result, item)
			filePath := normalizeFindingPath(projectDir, selectorString(result, doc.Parser.Findings.Mapping["file_path"]))
			resource := selectorString(merged, doc.Parser.Findings.Mapping["resource_ref"])
			severity := strings.ToLower(nonEmpty(selectorString(merged, doc.Parser.Findings.Mapping["severity"]), "info"))
			if mapped := doc.Parser.Severities[strings.ToUpper(severity)]; mapped != "" {
				severity = mapped
			}
			findingKey := selectorString(merged, doc.Parser.Findings.Mapping["finding_key"])
			if findingKey == "" {
				findingKey = resource
			}
			out = append(out, FindingUpsert{
				CheckType:     nonEmpty(selectorString(merged, doc.Parser.Findings.Mapping["check_type"]), "terraform.security.misconfig"),
				RuleID:        redactSensitiveText(nonEmpty(selectorString(merged, doc.Parser.Findings.Mapping["rule_id"]), doc.Tool+".unknown")),
				Severity:      severity,
				FilePath:      filePath,
				ResourceRef:   redactSensitiveText(resource),
				Title:         redactSensitiveText(nonEmpty(selectorString(merged, doc.Parser.Findings.Mapping["title"]), doc.Tool+" finding")),
				Description:   redactSensitiveText(selectorString(merged, doc.Parser.Findings.Mapping["description"])),
				Remediation:   redactSensitiveText(selectorString(merged, doc.Parser.Findings.Mapping["remediation"])),
				FindingKey:    redactSensitiveText(findingKey),
				RuleNamespace: redactSensitiveText(nonEmpty(selectorString(merged, doc.Parser.Findings.Mapping["rule_namespace"]), doc.Tool)),
			})
		}
	}
	return out
}

var (
	credentialURLRE  = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^@\s/]+@`)
	privateKeyBlock  = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	secretRefRE      = regexp.MustCompile(`(?i)secretref://[^\s"']+`)
	secretLikePairRE = regexp.MustCompile(`(?i)\b(token|password|passwd|secret|api[_-]?key|access[_-]?key)\s*[:=]\s*['"]?[^'",\s}]+`)
)

func redactSensitiveText(value string) string {
	if value == "" {
		return ""
	}
	value = privateKeyBlock.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	value = credentialURLRE.ReplaceAllString(value, "${1}[REDACTED]@")
	value = secretRefRE.ReplaceAllString(value, "secretref://[REDACTED]")
	value = secretLikePairRE.ReplaceAllString(value, "${1}=[REDACTED]")
	return value
}

func mergeObjects(left, right any) map[string]any {
	out := map[string]any{}
	if typed, ok := left.(map[string]any); ok {
		for key, value := range typed {
			out[key] = value
		}
	}
	if typed, ok := right.(map[string]any); ok {
		for key, value := range typed {
			out[key] = value
		}
	}
	return out
}

func profileChecksum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
