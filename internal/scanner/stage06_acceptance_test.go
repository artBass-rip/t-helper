package scanner_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/scanner"
)

func TestStage06ProjectScanSecurityValidationUsesToolProfilesAndStableFindings(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	installFakeToolchain(t)

	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:stage06",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), `provider "aws" {}`+"\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	project, _, err := scannerStore.UpsertProject(ctx, roots[0], "service", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}

	firstScanID := runStage06ProjectScan(t, ctx, runtime, scannerStore, jobStore, project, "12")
	page, err := scannerStore.ListFindings(ctx, scanner.FindingListOptions{ProjectScanID: firstScanID})
	if err != nil {
		t.Fatalf("list first findings: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("first scan findings = %d, want 1: %+v", len(page.Items), page.Items)
	}
	firstFinding := page.Items[0]
	if strings.Contains(string(firstFinding.FingerprintComponents), "12") {
		t.Fatalf("fingerprint components must not include source line: %s", firstFinding.FingerprintComponents)
	}

	secondScanID := runStage06ProjectScan(t, ctx, runtime, scannerStore, jobStore, project, "99")
	page, err = scannerStore.ListFindings(ctx, scanner.FindingListOptions{ProjectScanID: secondScanID})
	if err != nil {
		t.Fatalf("list second findings: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("second scan findings = %d, want stable upsert of 1: %+v", len(page.Items), page.Items)
	}
	if page.Items[0].ID != firstFinding.ID || page.Items[0].Fingerprint != firstFinding.Fingerprint {
		t.Fatalf("repeated scan did not update existing finding: first=%+v second=%+v", firstFinding, page.Items[0])
	}
	if strings.Contains(string(page.Items[0].FingerprintComponents), "99") {
		t.Fatalf("fingerprint components must not include shifted source line: %s", page.Items[0].FingerprintComponents)
	}

	scan, err := scannerStore.GetProjectScan(ctx, secondScanID)
	if err != nil {
		t.Fatalf("get second project scan: %v", err)
	}
	if scan.Status != jobs.StatusSucceeded {
		t.Fatalf("project scan aggregate status = %q, want succeeded", scan.Status)
	}
	var aggregate struct {
		Tools         []scanner.ToolMetadata `json:"tools"`
		Providers     []string               `json:"providers"`
		RequiredAuth  []string               `json:"required_auth"`
		CheckResults  []map[string]any       `json:"check_results"`
		ChildJobIDs   []string               `json:"child_job_ids"`
		SchemaVersion string                 `json:"schema_version"`
	}
	if err := json.Unmarshal(scan.ResultPayload, &aggregate); err != nil {
		t.Fatalf("decode aggregate: %v payload=%s", err, scan.ResultPayload)
	}
	if aggregate.SchemaVersion != scanner.ProjectScanAggregateSchema || len(aggregate.Tools) != 3 || len(aggregate.ChildJobIDs) != 1 {
		t.Fatalf("unexpected aggregate payload: %+v", aggregate)
	}
	if len(aggregate.Providers) != 1 || aggregate.Providers[0] != "aws" || len(aggregate.RequiredAuth) != 1 || aggregate.RequiredAuth[0] != "provider:aws" {
		t.Fatalf("providers/required_auth were not detected: %+v", aggregate)
	}
	for _, tool := range aggregate.Tools {
		if tool.ProfileID == "" || tool.CertificationStatus != "certified" {
			t.Fatalf("tool metadata does not come from certified active profile: %+v", tool)
		}
	}
}

func TestStage06ToolProfileValidateImportActivateAPIStore(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)

	payload := json.RawMessage(`{
	  "schema_version":"tool_profile.v1",
	  "tool":"terraform",
	  "profile_id":"terraform-validate-json-test",
	  "profile_version":"1.0.0",
	  "certified_versions":["1"],
	  "compatible_versions":["1"],
	  "version_policy":"certified_only",
	  "version_discovery":{"command":"terraform","args":["--version"]},
	  "scan_command":{"command":"terraform","args":["validate","-json","-no-color"]},
	  "parser":{"type":"json","result":"terraform_validate"},
	  "mapping":{"valid":{"path":"valid"},"errors_count":{"path":"error_count"},"warnings_count":{"path":"warning_count"}},
	  "redaction":{"max_output_bytes":1024}
	}`)
	validation, err := scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: payload, FixtureSet: "terraform-validate-success"})
	if err != nil {
		t.Fatalf("validate profile: %v", err)
	}
	if validation.ValidationStatus != "passed" || validation.Tool != "terraform" {
		t.Fatalf("unexpected validation result: %+v", validation)
	}
	for _, item := range []struct {
		profilePath string
		fixtureSet  string
		tool        string
	}{
		{"tool-profiles/terraform/terraform-validate-json-v1.json", "terraform-validate-success", "terraform"},
		{"tool-profiles/tflint/tflint-json-v1.json", "tflint-empty", "tflint"},
		{"tool-profiles/trivy/trivy-terraform-misconfig-json-v1.json", "trivy-misconfig", "trivy"},
	} {
		bundled := readRepoFile(t, item.profilePath)
		result, err := scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: json.RawMessage(bundled), FixtureSet: item.fixtureSet})
		if err != nil {
			t.Fatalf("validate bundled profile %s: %v", item.profilePath, err)
		}
		if result.ValidationStatus != "passed" || result.Tool != item.tool {
			t.Fatalf("bundled profile validation result for %s = %+v", item.profilePath, result)
		}
	}
	validation, err = scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: payload, FixtureSet: "trivy-misconfig"})
	if err != nil {
		t.Fatalf("validate profile with wrong fixture: %v", err)
	}
	if validation.ValidationStatus != "failed" {
		t.Fatalf("profile validation with mismatched fixture status = %q, want failed", validation.ValidationStatus)
	}
	imported, err := scannerStore.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: payload})
	if err != nil {
		t.Fatalf("import profile: %v", err)
	}
	if imported.Active {
		t.Fatalf("imported profile must remain inactive: %+v", imported)
	}
	activated, err := scannerStore.ActivateToolProfile(ctx, scanner.ToolProfileActivateInput{Tool: "terraform", ProfileID: imported.ProfileID, ProfileVersion: imported.ProfileVersion})
	if err != nil {
		t.Fatalf("activate profile: %v", err)
	}
	if !activated.Active {
		t.Fatalf("activated profile is not active: %+v", activated)
	}

	badProfile := terraformProfilePayload("terraform-network-url-test", "1.0.0", "certified_only", []string{"1"}, []string{"1"}, []string{"validate", "https://example.invalid/schema"})
	validation, err = scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: badProfile, FixtureSet: "terraform-validate-success"})
	if err != nil {
		t.Fatalf("validate bad profile: %v", err)
	}
	if validation.ValidationStatus != "failed" {
		t.Fatalf("network command profile validation status = %q, want failed", validation.ValidationStatus)
	}
	var failedValidations int
	if err := handle.DB.QueryRowContext(ctx, `SELECT count(*) FROM tool_profile_validation_results WHERE validation_status = 'failed'`).Scan(&failedValidations); err != nil {
		t.Fatalf("count failed profile validations: %v", err)
	}
	if failedValidations == 0 {
		t.Fatalf("failed profile validation was not persisted")
	}
}

func TestStage06ToolVersionPoliciesAndMissingBinary(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	installFakeToolchain(t)

	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: jobStore, Handlers: scanner.JobHandlers(scannerStore), WorkerID: "host:test:version-policy", Logger: slog.Default()})
	project := stage06ProjectFixture(t, ctx, scannerStore)

	uncertified := importAndActivateTerraformProfile(t, ctx, scannerStore, "terraform-uncertified-test", "certified_only", []string{"9"}, []string{"1"})
	job := runStage06ProjectScanJob(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformValidate, uncertified.ProfileID)
	if job.Status != jobs.StatusFailed || failureErrorCode(t, job) != "tool_version_uncertified" {
		t.Fatalf("uncertified version job = %s/%s, want failed/tool_version_uncertified", job.Status, failureErrorCode(t, job))
	}
	unsupported := importAndActivateTerraformProfile(t, ctx, scannerStore, "terraform-unsupported-test", "certified_only", []string{"9"}, []string{"8"})
	job = runStage06ProjectScanJob(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformValidate, unsupported.ProfileID)
	if job.Status != jobs.StatusFailed || failureErrorCode(t, job) != "tool_version_unsupported" {
		t.Fatalf("unsupported version job = %s/%s, want failed/tool_version_unsupported", job.Status, failureErrorCode(t, job))
	}

	compatible := importAndActivateTerraformProfile(t, ctx, scannerStore, "terraform-compatible-test", "compatible_range", []string{"9"}, []string{"1"})
	job = runStage06ProjectScanJob(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformValidate, compatible.ProfileID)
	if job.Status != jobs.StatusSucceeded {
		t.Fatalf("compatible version job status = %s: %s", job.Status, job.ErrorMessage)
	}
	var result scanner.ProjectScanResult
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		t.Fatalf("decode compatible result: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].CompatibilityStatus != "compatible" || result.Tools[0].CertificationStatus != "uncertified" {
		t.Fatalf("compatible metadata not marked uncertified: %+v", result.Tools)
	}

	bestEffort := importAndActivateTerraformProfile(t, ctx, scannerStore, "terraform-best-effort-test", "latest_best_effort", nil, nil)
	job = runStage06ProjectScanJob(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformValidate, bestEffort.ProfileID)
	if job.Status != jobs.StatusSucceeded {
		t.Fatalf("best-effort version job status = %s: %s", job.Status, job.ErrorMessage)
	}
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		t.Fatalf("decode best-effort result: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].CertificationStatus != "uncertified" {
		t.Fatalf("best-effort metadata not marked uncertified: %+v", result.Tools)
	}

	t.Setenv("PATH", t.TempDir())
	job = runStage06ProjectScanJob(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformValidate, bestEffort.ProfileID)
	if job.Status != jobs.StatusFailed || failureErrorCode(t, job) != "tool_not_found" {
		t.Fatalf("missing binary job = %s/%s, want failed/tool_not_found", job.Status, failureErrorCode(t, job))
	}
}

func TestStage06SecurityValidationRunsOnlyWhenEnabledInProjectSettings(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	installFakeToolchain(t)

	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: jobStore, Handlers: scanner.JobHandlers(scannerStore), WorkerID: "host:test:security-settings", Logger: slog.Default()})
	project := stage06ProjectFixture(t, ctx, scannerStore)
	securityEnabled := false
	if _, err := scannerStore.UpsertProjectSettings(ctx, project.ID, scanner.ProjectScanSettingsInput{Security: &struct {
		Enabled           *bool    `json:"enabled,omitempty"`
		EnabledModules    []string `json:"enabled_modules,omitempty"`
		ScheduleEnabled   *bool    `json:"schedule_enabled,omitempty"`
		ScheduleFrequency string   `json:"schedule_frequency,omitempty"`
		ValidateCode      *bool    `json:"validate_code,omitempty"`
	}{Enabled: &securityEnabled}}); err != nil {
		t.Fatalf("disable security settings: %v", err)
	}

	job := runStage06ProjectScanJob(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformFull, "security-disabled")
	if job.Status != jobs.StatusSucceeded {
		t.Fatalf("project scan with disabled security status = %s: %s", job.Status, job.ErrorMessage)
	}
	children, err := jobStore.List(ctx, jobs.ListFilters{JobGroupID: job.JobGroupID, ParentJobID: job.ID})
	if err != nil {
		t.Fatalf("list child jobs: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("security disabled should not enqueue child jobs: %+v", children)
	}
}

func TestStage06FindingFingerprintVariantsAndRedaction(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	project := stage06ProjectFixture(t, ctx, scannerStore)

	base := scanner.FindingUpsert{
		ProjectID:     project.ID,
		RuleSetID:     scanner.DefaultSecurityRuleSetID,
		CheckType:     "terraform.security.misconfig",
		RuleID:        "AVD-AWS-0088",
		Severity:      "high",
		FilePath:      "main.tf",
		ResourceRef:   "aws_s3_bucket.example",
		Title:         "token=abc123",
		Description:   "use https://user:pass@example.test/repo.git",
		Remediation:   "secretref://env/API_TOKEN",
		RuleNamespace: "trivy",
		Tool:          "trivy",
	}
	first, err := scannerStore.UpsertFinding(ctx, base)
	if err != nil {
		t.Fatalf("upsert base finding: %v", err)
	}
	if !strings.HasPrefix(first.Finding.Fingerprint, "fp:v1:") {
		t.Fatalf("fingerprint has unexpected format: %s", first.Finding.Fingerprint)
	}
	sum := sha256.Sum256(first.Finding.FingerprintComponents)
	if first.Finding.Fingerprint != "fp:v1:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint does not match stored canonical components: %s / %s", first.Finding.Fingerprint, first.Finding.FingerprintComponents)
	}
	for _, value := range []string{first.Finding.Title, first.Finding.Description, first.Finding.Remediation} {
		if strings.Contains(value, "abc123") || strings.Contains(value, "user:pass") || strings.Contains(value, "API_TOKEN") {
			t.Fatalf("finding text was not redacted: %+v", first.Finding)
		}
	}
	var components map[string]any
	if err := json.Unmarshal(first.Finding.FingerprintComponents, &components); err != nil {
		t.Fatalf("decode fingerprint components: %v", err)
	}
	for _, forbidden := range []string{"job_id", "project_scan_id", "line", "column", "title", "description", "remediation", "severity"} {
		if _, ok := components[forbidden]; ok {
			t.Fatalf("fingerprint components include forbidden field %s: %s", forbidden, first.Finding.FingerprintComponents)
		}
	}

	changedRuleSet := base
	otherActive := false
	otherRuleSet, err := scannerStore.UpsertRuleSet(ctx, scanner.SecurityRuleSetInput{Name: "Other", Version: "1.0.0", SourceType: "bundled", Active: &otherActive})
	if err != nil {
		t.Fatalf("upsert other rule set: %v", err)
	}
	changedRuleSet.RuleSetID = otherRuleSet.ID
	second, err := scannerStore.UpsertFinding(ctx, changedRuleSet)
	if err != nil {
		t.Fatalf("upsert changed rule set finding: %v", err)
	}
	if second.Finding.Fingerprint == first.Finding.Fingerprint {
		t.Fatalf("changed rule_set_id must produce different fingerprint")
	}
	workspace := base
	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := handle.DB.ExecContext(ctx, `INSERT INTO environments (id, name, code, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "env_stage06_fp", "Dev", "dev", nowText, nowText); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `INSERT INTO workspaces (id, project_id, environment_id, name, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "workspace_stage06_fp", project.ID, "env_stage06_fp", "dev", 0, nowText, nowText); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	workspace.WorkspaceID = "workspace_stage06_fp"
	third, err := scannerStore.UpsertFinding(ctx, workspace)
	if err != nil {
		t.Fatalf("upsert workspace finding: %v", err)
	}
	if third.Finding.Fingerprint == first.Finding.Fingerprint {
		t.Fatalf("workspace-specific finding must produce different fingerprint")
	}
	renamed := base
	renamed.FilePath = "renamed/main.tf"
	fourth, err := scannerStore.UpsertFinding(ctx, renamed)
	if err != nil {
		t.Fatalf("upsert renamed finding: %v", err)
	}
	if fourth.Finding.Fingerprint == first.Finding.Fingerprint {
		t.Fatalf("renamed file without documented stable move key must produce different fingerprint")
	}
	keyOnly := base
	keyOnly.ResourceRef = ""
	keyOnly.FindingKey = "stable-key"
	if _, err := scannerStore.UpsertFinding(ctx, keyOnly); err != nil {
		t.Fatalf("finding with stable finding_key should persist: %v", err)
	}
	noIdentity := base
	noIdentity.ResourceRef = ""
	noIdentity.FindingKey = ""
	if _, err := scannerStore.UpsertFinding(ctx, noIdentity); err == nil {
		t.Fatalf("finding without resource_ref or finding_key should be rejected")
	}
}

func runStage06ProjectScan(t *testing.T, ctx context.Context, runtime *jobs.Runtime, scannerStore *scanner.Store, jobStore *jobs.Store, project scanner.Project, line string) string {
	t.Helper()
	if err := os.Setenv("T_HELPER_FAKE_TRIVY_LINE", line); err != nil {
		t.Fatalf("set fake trivy line: %v", err)
	}
	projectScanID := "project_scan_" + jobs.NewJobID()
	payload, err := json.Marshal(scanner.ProjectScanPayload{
		SchemaVersion: scanner.ProjectScanPayloadSchema,
		ProjectID:     project.ID,
		ProjectScanID: projectScanID,
		ScanType:      scanner.ScanTypeTerraformFull,
		RuleSetID:     scanner.DefaultSecurityRuleSetID,
		Reason:        "test",
	})
	if err != nil {
		t.Fatalf("marshal project scan payload: %v", err)
	}
	ref, err := jobStore.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:    "project_scan",
		JobGroupID: "project_scan:" + projectScanID,
		WorkflowID: projectScanID,
		LockKey:    "project_scan:" + project.ID,
		Payload:    payload,
	})
	if err != nil {
		t.Fatalf("enqueue project scan: %v", err)
	}
	if _, err := scannerStore.CreateProjectScan(ctx, project, projectScanID, scanner.ScanTypeTerraformFull, scanner.DefaultSecurityRuleSetID, ref.JobID); err != nil {
		t.Fatalf("create project scan row: %v", err)
	}
	runUntilComplete(t, ctx, runtime, jobStore, ref.JobID)
	children, err := jobStore.List(ctx, jobs.ListFilters{JobGroupID: "project_scan:" + projectScanID, ParentJobID: ref.JobID})
	if err != nil {
		t.Fatalf("list child jobs: %v", err)
	}
	if len(children) != 1 || children[0].JobType != "security_validation_scan" {
		t.Fatalf("expected one security child job, got %+v", children)
	}
	runUntilComplete(t, ctx, runtime, jobStore, children[0].ID)
	if err := jobStore.ReconcileWorkflowStatuses(ctx); err != nil {
		t.Fatalf("reconcile workflows: %v", err)
	}
	if err := scannerStore.ApplyWorkflowAggregateToProjectScan(ctx, "project_scan:"+projectScanID); err != nil {
		t.Fatalf("apply aggregate: %v", err)
	}
	return projectScanID
}

func runStage06ProjectScanJob(t *testing.T, ctx context.Context, runtime *jobs.Runtime, scannerStore *scanner.Store, jobStore *jobs.Store, project scanner.Project, scanType, profileID string) jobs.Job {
	t.Helper()
	projectScanID := "project_scan_" + jobs.NewJobID()
	payload, err := json.Marshal(scanner.ProjectScanPayload{
		SchemaVersion: scanner.ProjectScanPayloadSchema,
		ProjectID:     project.ID,
		ProjectScanID: projectScanID,
		ScanType:      scanType,
		Reason:        "test",
	})
	if err != nil {
		t.Fatalf("marshal project scan payload: %v", err)
	}
	ref, err := jobStore.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:    "project_scan",
		JobGroupID: "project_scan:" + projectScanID,
		WorkflowID: projectScanID,
		LockKey:    "project_scan:" + project.ID + ":" + profileID,
		Payload:    payload,
	})
	if err != nil {
		t.Fatalf("enqueue project scan: %v", err)
	}
	if _, err := scannerStore.CreateProjectScan(ctx, project, projectScanID, scanType, "", ref.JobID); err != nil {
		t.Fatalf("create project scan row: %v", err)
	}
	return runUntilTerminal(t, ctx, runtime, jobStore, ref.JobID)
}

func stage06ProjectFixture(t *testing.T, ctx context.Context, scannerStore *scanner.Store) scanner.Project {
	t.Helper()
	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), `provider "aws" {}`+"\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	project, _, err := scannerStore.UpsertProject(ctx, roots[0], "service", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	return project
}

func importAndActivateTerraformProfile(t *testing.T, ctx context.Context, store *scanner.Store, profileID, policy string, certified, compatible []string) scanner.ToolProfile {
	t.Helper()
	imported, err := store.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: terraformProfilePayload(profileID, "1.0.0", policy, certified, compatible, []string{"validate", "-json", "-no-color"})})
	if err != nil {
		t.Fatalf("import %s: %v", profileID, err)
	}
	activated, err := store.ActivateToolProfile(ctx, scanner.ToolProfileActivateInput{Tool: "terraform", ProfileID: imported.ProfileID, ProfileVersion: imported.ProfileVersion})
	if err != nil {
		t.Fatalf("activate %s: %v", profileID, err)
	}
	return activated
}

func terraformProfilePayload(profileID, version, policy string, certified, compatible, scanArgs []string) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"schema_version":      "tool_profile.v1",
		"tool":                "terraform",
		"profile_id":          profileID,
		"profile_version":     version,
		"certified_versions":  certified,
		"compatible_versions": compatible,
		"version_policy":      policy,
		"version_discovery":   map[string]any{"command": "terraform", "args": []string{"--version"}},
		"scan_command":        map[string]any{"command": "terraform", "args": scanArgs},
		"parser":              map[string]any{"type": "json", "result": "terraform_validate"},
		"mapping":             map[string]any{"valid": map[string]any{"path": "valid"}, "errors_count": map[string]any{"path": "error_count"}, "warnings_count": map[string]any{"path": "warning_count"}},
		"redaction":           map[string]any{"max_output_bytes": 1024},
	})
	return json.RawMessage(payload)
}

func installFakeToolchain(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	for _, tool := range []string{"terraform", "tflint", "trivy"} {
		path := filepath.Join(binDir, tool)
		if err := os.WriteFile(path, []byte(fakeToolScript(tool)), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", tool, err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func fakeToolScript(tool string) string {
	switch tool {
	case "terraform":
		return "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Terraform v1.8.5'; exit 0; fi\necho '{\"valid\":true,\"error_count\":0,\"warning_count\":0,\"diagnostics\":[]}'\n"
	case "tflint":
		return "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'TFLint version 0.50.0'; exit 0; fi\necho '{\"issues\":[]}'\n"
	default:
		return "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Version: 0.60.0'; exit 0; fi\nline=${T_HELPER_FAKE_TRIVY_LINE:-12}\nprintf '{\"Results\":[{\"Target\":\"main.tf\",\"Misconfigurations\":[{\"ID\":\"AVD-AWS-0088\",\"Title\":\"Public bucket\",\"Description\":\"desc\",\"Resolution\":\"fix\",\"Severity\":\"HIGH\",\"CauseMetadata\":{\"Resource\":\"aws_s3_bucket.example\",\"Provider\":\"aws\",\"Service\":\"s3\",\"StartLine\":%s}}]}]}' \"$line\"\n"
	}
}

func readRepoFile(t *testing.T, relative string) []byte {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		path := filepath.Join(dir, relative)
		raw, err := os.ReadFile(path)
		if err == nil {
			return raw
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo file %s not found", relative)
		}
		dir = parent
	}
}
