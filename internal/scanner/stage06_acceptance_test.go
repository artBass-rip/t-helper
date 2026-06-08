package scanner_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
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
		Store:         jobStore,
		Handlers:      scanner.JobHandlers(scannerStore),
		AfterComplete: scanner.JobCompletionHook(scannerStore),
		WorkerID:      "host:test:stage06",
		Logger:        slog.Default(),
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
	assertNoLocationFingerprintComponents(t, firstFinding.FingerprintComponents)

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
	assertNoLocationFingerprintComponents(t, page.Items[0].FingerprintComponents)

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
		JobGroupID    string                 `json:"job_group_id"`
		ParentJobID   string                 `json:"parent_job_id"`
		ChildJobIDs   []string               `json:"child_job_ids"`
		SchemaVersion string                 `json:"schema_version"`
	}
	if err := json.Unmarshal(scan.ResultPayload, &aggregate); err != nil {
		t.Fatalf("decode aggregate: %v payload=%s", err, scan.ResultPayload)
	}
	if aggregate.SchemaVersion != scanner.ProjectScanAggregateSchema || len(aggregate.Tools) != 3 || len(aggregate.ChildJobIDs) != 1 {
		t.Fatalf("unexpected aggregate payload: %+v", aggregate)
	}
	if aggregate.JobGroupID != "project_scan:"+scan.ID || aggregate.ParentJobID != scan.JobID {
		t.Fatalf("aggregate job identifiers do not match project scan: %+v scan=%+v", aggregate, scan)
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

func assertNoLocationFingerprintComponents(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var components map[string]any
	if err := json.Unmarshal(raw, &components); err != nil {
		t.Fatalf("decode fingerprint components: %v", err)
	}
	for _, key := range []string{"line", "column", "start_line", "end_line", "start_column", "end_column"} {
		if _, ok := components[key]; ok {
			t.Fatalf("fingerprint components include location field %q: %s", key, raw)
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
		{"tool-profiles/terraform/terraform-validate-json-v1.json", "terraform-validate-errors", "terraform"},
		{"tool-profiles/tflint/tflint-json-v1.json", "tflint-empty", "tflint"},
		{"tool-profiles/tflint/tflint-json-v1.json", "tflint-warning", "tflint"},
		{"tool-profiles/trivy/trivy-terraform-misconfig-json-v1.json", "trivy-misconfig", "trivy"},
		{"tool-profiles/trivy/trivy-terraform-misconfig-json-v1.json", "trivy-secret-redaction", "trivy"},
		{"tool-profiles/trivy/trivy-terraform-misconfig-json-v1.json", "trivy-malformed-output", "trivy"},
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
	for name, invalidPayload := range map[string]json.RawMessage{
		"missing-required-fields": json.RawMessage(`{"schema_version":"tool_profile.v1","tool":"terraform"}`),
		"unsupported-schema":      json.RawMessage(`{"schema_version":"tool_profile.v2","tool":"terraform","profile_id":"bad","profile_version":"1.0.0"}`),
	} {
		validation, err = scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: invalidPayload})
		if err != nil {
			t.Fatalf("validate invalid profile %s: %v", name, err)
		}
		if validation.ValidationStatus != "failed" {
			t.Fatalf("invalid profile %s validation status = %q, want failed", name, validation.ValidationStatus)
		}
	}
	imported, err := scannerStore.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: payload})
	if err != nil {
		t.Fatalf("import profile: %v", err)
	}
	if imported.Active {
		t.Fatalf("imported profile must remain inactive: %+v", imported)
	}
	var linkedValidations int
	if err := handle.DB.QueryRowContext(ctx, `SELECT count(*) FROM tool_profile_validation_results WHERE tool_profile_id = ? AND validation_status = 'passed'`, imported.ID).Scan(&linkedValidations); err != nil {
		t.Fatalf("count linked profile validations: %v", err)
	}
	if linkedValidations == 0 {
		t.Fatalf("imported profile was not linked to its passed pre-import validation")
	}
	activated, err := scannerStore.ActivateToolProfile(ctx, scanner.ToolProfileActivateInput{Tool: "terraform", ProfileID: imported.ProfileID, ProfileVersion: imported.ProfileVersion})
	if err != nil {
		t.Fatalf("activate profile: %v", err)
	}
	if !activated.Active {
		t.Fatalf("activated profile is not active: %+v", activated)
	}

	unvalidatedProfile := terraformProfilePayload("terraform-unvalidated-test", "1.0.0", "certified_only", []string{"1"}, []string{"1"}, []string{"validate", "-json", "-no-color"})
	unvalidated, err := scannerStore.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: unvalidatedProfile})
	if err != nil {
		t.Fatalf("import unvalidated profile: %v", err)
	}
	if _, err := scannerStore.ActivateToolProfile(ctx, scanner.ToolProfileActivateInput{Tool: "terraform", ProfileID: unvalidated.ProfileID, ProfileVersion: unvalidated.ProfileVersion}); err == nil {
		t.Fatalf("unvalidated non-bundled profile activation succeeded")
	}

	candidate, err := scannerStore.AnalyzeToolProfile(ctx, scanner.ToolProfileAnalyzeInput{
		BaselineProfileID: "terraform-validate-json-v1",
		SamplePayload:     json.RawMessage(`{"valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`),
	})
	if err != nil {
		t.Fatalf("analyze tool profile: %v", err)
	}
	if candidate.SourceType != "generated_candidate" || candidate.Confidence == "" || len(candidate.ProfilePayload) == 0 || len(candidate.FixturePayload) == 0 {
		t.Fatalf("unexpected analyzer candidate: %+v", candidate)
	}
	generated, err := scannerStore.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: candidate.ProfilePayload, SourceType: candidate.SourceType})
	if err != nil {
		t.Fatalf("import generated candidate: %v", err)
	}
	if generated.SourceType != "generated_candidate" || generated.Active {
		t.Fatalf("generated candidate import state = %+v", generated)
	}
	if _, err := scannerStore.ActivateToolProfile(ctx, scanner.ToolProfileActivateInput{Tool: generated.Tool, ProfileID: generated.ProfileID, ProfileVersion: generated.ProfileVersion}); err == nil {
		t.Fatalf("unvalidated generated candidate activation succeeded")
	}
	candidateFixturePath := filepath.Join(t.TempDir(), "candidate-fixture.json")
	mustWriteFile(t, candidateFixturePath, string(candidate.FixturePayload))
	candidateValidation, err := scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: candidate.ProfilePayload, FixtureSet: candidateFixturePath})
	if err != nil {
		t.Fatalf("validate generated candidate: %v", err)
	}
	if candidateValidation.ValidationStatus != "passed" {
		t.Fatalf("generated candidate validation = %q: %s", candidateValidation.ValidationStatus, candidateValidation.Diagnostics)
	}
	generated, err = scannerStore.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: candidate.ProfilePayload, SourceType: candidate.SourceType})
	if err != nil {
		t.Fatalf("re-import validated generated candidate: %v", err)
	}
	activatedGenerated, err := scannerStore.ActivateToolProfile(ctx, scanner.ToolProfileActivateInput{Tool: generated.Tool, ProfileID: generated.ProfileID, ProfileVersion: generated.ProfileVersion})
	if err != nil {
		t.Fatalf("activate validated generated candidate: %v", err)
	}
	if !activatedGenerated.Active {
		t.Fatalf("validated generated candidate was not activated: %+v", activatedGenerated)
	}

	secretCandidate, err := scannerStore.AnalyzeToolProfile(ctx, scanner.ToolProfileAnalyzeInput{
		BaselineProfileID: "trivy-terraform-misconfig-json-v1",
		SamplePayload: json.RawMessage(
			`{"Results":[{"Target":"main.tf","Misconfigurations":[{"ID":"AVD-AWS-9999","Title":"Secret finding","Description":"api_key=supersecret token:abcd1234","Resolution":"Rotate https://user:pass@example.invalid/path and use secretref://env/API_TOKEN","Severity":"CRITICAL","CauseMetadata":{"Resource":"aws_instance.secret","Provider":"aws","Service":"ec2","StartLine":1}}]}]}`,
		),
	})
	if err != nil {
		t.Fatalf("analyze secret-like trivy sample: %v", err)
	}
	secretFixtureText := string(secretCandidate.FixturePayload)
	for _, leaked := range []string{"supersecret", "abcd1234", "user:pass", "secretref://env/API_TOKEN"} {
		if strings.Contains(secretFixtureText, leaked) {
			t.Fatalf("analyzer fixture payload leaked %q: %s", leaked, secretFixtureText)
		}
	}
	secretCandidateFixturePath := filepath.Join(t.TempDir(), "secret-candidate-fixture.json")
	mustWriteFile(t, secretCandidateFixturePath, secretFixtureText)
	secretCandidateValidation, err := scannerStore.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: secretCandidate.ProfilePayload, FixtureSet: secretCandidateFixturePath})
	if err != nil {
		t.Fatalf("validate secret-like generated candidate: %v", err)
	}
	if secretCandidateValidation.ValidationStatus != "passed" {
		t.Fatalf("secret-like generated candidate validation = %q: %s", secretCandidateValidation.ValidationStatus, secretCandidateValidation.Diagnostics)
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
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: jobStore, Handlers: scanner.JobHandlers(scannerStore), AfterComplete: scanner.JobCompletionHook(scannerStore), WorkerID: "host:test:version-policy", Logger: slog.Default()})
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
	job, failedScanID := runStage06ProjectScanJobWithID(t, ctx, runtime, scannerStore, jobStore, project, scanner.ScanTypeTerraformValidate, bestEffort.ProfileID)
	if job.Status != jobs.StatusFailed || failureErrorCode(t, job) != "tool_not_found" {
		t.Fatalf("missing binary job = %s/%s, want failed/tool_not_found", job.Status, failureErrorCode(t, job))
	}
	failedScan, err := scannerStore.GetProjectScan(ctx, failedScanID)
	if err != nil {
		t.Fatalf("get failed project scan: %v", err)
	}
	if failedScan.Status != jobs.StatusFailed || !strings.Contains(failedScan.ErrorMessage, "binary is unavailable") {
		t.Fatalf("failed project scan aggregate = status %q error %q", failedScan.Status, failedScan.ErrorMessage)
	}
	var aggregate struct {
		Errors []struct {
			JobID     string `json:"job_id"`
			JobType   string `json:"job_type"`
			Status    string `json:"status"`
			ErrorCode string `json:"error_code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(failedScan.ResultPayload, &aggregate); err != nil {
		t.Fatalf("decode failed scan aggregate: %v payload=%s", err, failedScan.ResultPayload)
	}
	if len(aggregate.Errors) != 1 || aggregate.Errors[0].ErrorCode != "tool_not_found" || aggregate.Errors[0].JobID != job.ID || aggregate.Errors[0].JobType != "project_scan" {
		t.Fatalf("failed scan aggregate errors = %+v", aggregate.Errors)
	}
}

func TestStage06SecurityValidationRunsOnlyWhenEnabledInProjectSettings(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	installFakeToolchain(t)

	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{Store: jobStore, Handlers: scanner.JobHandlers(scannerStore), AfterComplete: scanner.JobCompletionHook(scannerStore), WorkerID: "host:test:security-settings", Logger: slog.Default()})
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

func TestStage06ProjectSecurityModulesUseCanonicalConfigKey(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	scannerStore := scanner.NewStore(handle)
	project := stage06ProjectFixture(t, ctx, scannerStore)
	upsertSecurityModulesConfig(t, ctx, handle.DB, []string{"checkov", "checkov"})

	modules, err := scannerStore.ConfigSecurityModules(ctx)
	if err != nil {
		t.Fatalf("config security modules: %v", err)
	}
	if len(modules) != 1 || modules[0] != "checkov" {
		t.Fatalf("security modules = %+v, want [checkov]", modules)
	}
	if _, err := scannerStore.UpsertProjectSettings(ctx, project.ID, scanner.ProjectScanSettingsInput{Security: &struct {
		Enabled           *bool    `json:"enabled,omitempty"`
		EnabledModules    []string `json:"enabled_modules,omitempty"`
		ScheduleEnabled   *bool    `json:"schedule_enabled,omitempty"`
		ScheduleFrequency string   `json:"schedule_frequency,omitempty"`
		ValidateCode      *bool    `json:"validate_code,omitempty"`
	}{EnabledModules: []string{"trivy"}}}); err == nil {
		t.Fatalf("trivy should be rejected when canonical security module config allows only checkov")
	}
	settings, err := scannerStore.UpsertProjectSettings(ctx, project.ID, scanner.ProjectScanSettingsInput{Security: &struct {
		Enabled           *bool    `json:"enabled,omitempty"`
		EnabledModules    []string `json:"enabled_modules,omitempty"`
		ScheduleEnabled   *bool    `json:"schedule_enabled,omitempty"`
		ScheduleFrequency string   `json:"schedule_frequency,omitempty"`
		ValidateCode      *bool    `json:"validate_code,omitempty"`
	}{EnabledModules: []string{"checkov", "checkov"}}})
	if err != nil {
		t.Fatalf("checkov should be accepted from canonical security module config: %v", err)
	}
	if len(settings.Security.EnabledModules) != 1 || settings.Security.EnabledModules[0] != "checkov" {
		t.Fatalf("project security modules = %+v, want [checkov]", settings.Security.EnabledModules)
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
	if !first.Finding.FirstSeenAt.Equal(first.Finding.DetectedAt) {
		t.Fatalf("new finding detected_at = %s, want first_seen_at %s", first.Finding.DetectedAt, first.Finding.FirstSeenAt)
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
	time.Sleep(time.Millisecond)
	repeated := base
	repeatedJob, err := jobs.NewStore(handle).Enqueue(ctx, jobs.EnqueueRequest{JobType: "config_reload", Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1"}`)})
	if err != nil {
		t.Fatalf("enqueue repeated finding job: %v", err)
	}
	repeated.JobID = repeatedJob.JobID
	repeated.Severity = "critical"
	updated, err := scannerStore.UpsertFinding(ctx, repeated)
	if err != nil {
		t.Fatalf("upsert repeated finding: %v", err)
	}
	if updated.Created || updated.Finding.ID != first.Finding.ID {
		t.Fatalf("repeated finding should update existing row: %+v first=%+v", updated, first.Finding)
	}
	if !updated.Finding.FirstSeenAt.Equal(first.Finding.FirstSeenAt) || !updated.Finding.DetectedAt.Equal(first.Finding.DetectedAt) {
		t.Fatalf("repeated finding changed initial timestamps: first=%s/%s updated=%s/%s", first.Finding.FirstSeenAt, first.Finding.DetectedAt, updated.Finding.FirstSeenAt, updated.Finding.DetectedAt)
	}
	if !updated.Finding.LastSeenAt.After(first.Finding.LastSeenAt) {
		t.Fatalf("repeated finding last_seen_at = %s, want after %s", updated.Finding.LastSeenAt, first.Finding.LastSeenAt)
	}
	if updated.Finding.Severity != "critical" || updated.Finding.JobID != repeated.JobID {
		t.Fatalf("repeated finding did not update mutable fields: %+v", updated.Finding)
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
	unsafePath := base
	unsafePath.FilePath = filepath.Join(t.TempDir(), "..", "outside", "main.tf")
	unsafePath.FindingKey = "unsafe-path"
	unsafeFinding, err := scannerStore.UpsertFinding(ctx, unsafePath)
	if err != nil {
		t.Fatalf("upsert unsafe path finding: %v", err)
	}
	if strings.Contains(unsafeFinding.Finding.FilePath, "..") || filepath.IsAbs(unsafeFinding.Finding.FilePath) {
		t.Fatalf("unsafe finding path was persisted: %q", unsafeFinding.Finding.FilePath)
	}
	var unsafeComponents map[string]any
	if err := json.Unmarshal(unsafeFinding.Finding.FingerprintComponents, &unsafeComponents); err != nil {
		t.Fatalf("decode unsafe fingerprint components: %v", err)
	}
	if normalized, _ := unsafeComponents["normalized_file_path"].(string); strings.Contains(normalized, "..") || strings.HasPrefix(normalized, "/") {
		t.Fatalf("unsafe normalized fingerprint path = %q components=%s", normalized, unsafeFinding.Finding.FingerprintComponents)
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
	afterParent, err := scannerStore.GetProjectScan(ctx, projectScanID)
	if err != nil {
		t.Fatalf("get project scan after parent: %v", err)
	}
	if afterParent.Status != jobs.StatusRunning {
		t.Fatalf("project scan status after parent completion = %q, want running until child completion", afterParent.Status)
	}
	runUntilComplete(t, ctx, runtime, jobStore, children[0].ID)
	return projectScanID
}

func runStage06ProjectScanJob(t *testing.T, ctx context.Context, runtime *jobs.Runtime, scannerStore *scanner.Store, jobStore *jobs.Store, project scanner.Project, scanType, profileID string) jobs.Job {
	t.Helper()
	job, _ := runStage06ProjectScanJobWithID(t, ctx, runtime, scannerStore, jobStore, project, scanType, profileID)
	return job
}

func runStage06ProjectScanJobWithID(t *testing.T, ctx context.Context, runtime *jobs.Runtime, scannerStore *scanner.Store, jobStore *jobs.Store, project scanner.Project, scanType, profileID string) (jobs.Job, string) {
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
	return runUntilTerminal(t, ctx, runtime, jobStore, ref.JobID), projectScanID
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
	payload := terraformProfilePayload(profileID, "1.0.0", policy, certified, compatible, []string{"validate", "-json", "-no-color"})
	validation, err := store.ValidateToolProfile(ctx, scanner.ToolProfileValidateInput{ProfilePayload: payload, FixtureSet: "terraform-validate-success"})
	if err != nil {
		t.Fatalf("validate %s: %v", profileID, err)
	}
	if validation.ValidationStatus != "passed" {
		t.Fatalf("validate %s status = %q, want passed: %s", profileID, validation.ValidationStatus, validation.Diagnostics)
	}
	imported, err := store.ImportToolProfile(ctx, scanner.ToolProfileImportInput{ProfilePayload: payload})
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

func upsertSecurityModulesConfig(t *testing.T, ctx context.Context, handle interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, modules []string) {
	t.Helper()
	raw, err := json.Marshal(modules)
	if err != nil {
		t.Fatalf("marshal security modules config: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = handle.ExecContext(ctx, `INSERT INTO config_entries (id, key, value, value_type, scope, version, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key, scope) DO UPDATE SET value = excluded.value, value_type = excluded.value_type, version = config_entries.version + 1, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		"cfg_scanning_security_scan_modules", "scanning.security_scan.modules", string(raw), "json", "system", 1, now, "test")
	if err != nil {
		t.Fatalf("upsert security modules config: %v", err)
	}
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
