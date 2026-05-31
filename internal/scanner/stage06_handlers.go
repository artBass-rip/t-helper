package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/artBass-rip/t-helper/internal/jobs"
)

type projectScanHandler struct {
	store *Store
}

type securityValidationHandler struct {
	store *Store
}

func (h projectScanHandler) Handle(ctx context.Context, env jobs.HandlerEnv, job jobs.Job) (json.RawMessage, error) {
	var payload ProjectScanPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if payload.SchemaVersion != ProjectScanPayloadSchema {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "invalid project scan payload schema_version", Retryable: false}
	}
	project, err := h.store.GetProject(ctx, payload.ProjectID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if err := h.store.MarkProjectScanRunning(ctx, payload.ProjectScanID); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	result := ProjectScanResult{
		SchemaVersion: ProjectScanResultSchema,
		ProjectID:     project.ID,
		ProjectScanID: payload.ProjectScanID,
		Tools:         []ToolMetadata{},
		Providers:     []string{},
		RequiredAuth:  []string{},
		CheckResults:  []any{},
	}
	if shouldRunTerraformValidate(payload.ScanType) {
		tool, check, err := runTerraformValidate(ctx, h.store, project.Path)
		if err != nil {
			return nil, classifyToolError(err)
		}
		result.Tools = append(result.Tools, tool)
		result.CheckResults = append(result.CheckResults, check)
	}
	if shouldRunTFLint(payload.ScanType) {
		tool, check, err := runTFLint(ctx, h.store, project.Path)
		if err != nil {
			return nil, classifyToolError(err)
		}
		result.Tools = append(result.Tools, tool)
		result.CheckResults = append(result.CheckResults, check)
	}
	settings, err := h.store.GetProjectSettings(ctx, project.ID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	if settings.Security.Enabled && len(settings.Security.EnabledModules) > 0 && shouldRunSecurity(payload.ScanType) {
		childPayload, err := marshalJSON(SecurityScanPayload{
			SchemaVersion:  SecurityScanPayloadSchema,
			ProjectID:      project.ID,
			ProjectScanID:  payload.ProjectScanID,
			RuleSetID:      payload.RuleSetID,
			EnabledModules: settings.Security.EnabledModules,
			Reason:         nonEmpty(payload.Reason, "project_scan"),
		})
		if err != nil {
			return nil, jobs.HandlerError{Code: "serialization_error", Message: err.Error(), Retryable: false}
		}
		ref, err := env.Store.Enqueue(ctx, jobs.EnqueueRequest{
			JobType:     "security_validation_scan",
			Actor:       nonEmpty(job.Actor, "project-scanner"),
			ParentJobID: job.ID,
			JobGroupID:  job.JobGroupID,
			WorkflowID:  payload.ProjectScanID,
			LockKey:     "security_validation_scan:" + payload.ProjectScanID,
			Payload:     childPayload,
		})
		if err != nil {
			return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
		}
		result.SecurityValidationRequested = true
		result.SecurityValidationJobID = ref.JobID
		_ = env.EmitChildCreated(ctx, job, "security validation job enqueued", map[string]any{
			"project_id":      project.ID,
			"project_scan_id": payload.ProjectScanID,
			"job_id":          ref.JobID,
		})
	}
	raw, err := marshalJSON(result)
	if err != nil {
		return nil, jobs.HandlerError{Code: "serialization_error", Message: err.Error(), Retryable: false}
	}
	_ = h.store.ApplyWorkflowAggregateToProjectScan(ctx, job.JobGroupID)
	return raw, nil
}

func (h securityValidationHandler) Handle(ctx context.Context, env jobs.HandlerEnv, job jobs.Job) (json.RawMessage, error) {
	var payload SecurityScanPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if payload.SchemaVersion != SecurityScanPayloadSchema {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "invalid security validation payload schema_version", Retryable: false}
	}
	project, err := h.store.GetProject(ctx, payload.ProjectID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	result := SecurityScanResult{
		SchemaVersion: SecurityScanResultSchema,
		ProjectID:     project.ID,
		ProjectScanID: payload.ProjectScanID,
		Tools:         []ToolMetadata{},
	}
	for _, module := range payload.EnabledModules {
		if module != DefaultSecurityModuleTrivy {
			result.ModulesFailed++
			continue
		}
		tool, findings, err := runTrivy(ctx, h.store, project.Path)
		if err != nil {
			return nil, classifyToolError(err)
		}
		result.Tools = append(result.Tools, tool)
		for _, finding := range findings {
			finding.ProjectID = project.ID
			finding.RepositoryID = project.RepositoryID
			finding.JobID = job.ID
			finding.RuleSetID = payload.RuleSetID
			finding.Tool = DefaultSecurityModuleTrivy
			upserted, err := h.store.UpsertFinding(ctx, finding)
			if err != nil {
				return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
			}
			if upserted.Created {
				result.FindingsCreated++
			} else {
				result.FindingsUpdated++
			}
		}
		result.ModulesSucceeded++
	}
	raw, err := marshalJSON(result)
	if err != nil {
		return nil, jobs.HandlerError{Code: "serialization_error", Message: err.Error(), Retryable: false}
	}
	_ = h.store.ApplyWorkflowAggregateToProjectScan(ctx, job.JobGroupID)
	_ = env.EmitProgress(ctx, job, "security validation completed", map[string]any{
		"project_scan_id":  payload.ProjectScanID,
		"findings_created": result.FindingsCreated,
		"findings_updated": result.FindingsUpdated,
	})
	return raw, nil
}

func shouldRunTerraformValidate(scanType string) bool {
	return scanType == ScanTypeTerraformValidate || scanType == ScanTypeTerraformFull
}

func shouldRunTFLint(scanType string) bool {
	return scanType == ScanTypeTerraformStatic || scanType == ScanTypeTerraformFull
}

func shouldRunSecurity(scanType string) bool {
	return scanType == ScanTypeTerraformSecurity || scanType == ScanTypeTerraformFull || scanType == ScanTypeSecurityValidation
}

type toolRunError struct {
	code      string
	message   string
	retryable bool
}

func (e toolRunError) Error() string {
	return e.message
}

func classifyToolError(err error) error {
	var toolErr toolRunError
	if errors.As(err, &toolErr) {
		return jobs.HandlerError{Code: toolErr.code, Message: toolErr.message, Retryable: toolErr.retryable}
	}
	return jobs.HandlerError{Code: "tool_failed", Message: err.Error(), Retryable: false}
}

func runTerraformValidate(ctx context.Context, store *Store, dir string) (ToolMetadata, map[string]any, error) {
	meta, err := store.toolMetadata(ctx, "terraform")
	if err != nil {
		return meta, nil, err
	}
	cmd := exec.CommandContext(ctx, "terraform", "validate", "-json", "-no-color")
	cmd.Dir = dir
	output, runErr := cmd.Output()
	var parsed struct {
		Valid        bool `json:"valid"`
		ErrorCount   int  `json:"error_count"`
		WarningCount int  `json:"warning_count"`
		Diagnostics  []struct {
			Severity string `json:"severity"`
			Summary  string `json:"summary"`
		} `json:"diagnostics"`
	}
	if len(output) > 0 {
		_ = json.Unmarshal(output, &parsed)
	}
	check := map[string]any{
		"tool":           "terraform",
		"check_type":     "terraform.validate",
		"valid":          parsed.Valid,
		"errors_count":   parsed.ErrorCount,
		"warnings_count": parsed.WarningCount,
	}
	if runErr != nil && len(output) == 0 {
		return meta, check, toolRunError{code: "tool_failed", message: "terraform validate failed", retryable: false}
	}
	return meta, check, nil
}

func runTFLint(ctx context.Context, store *Store, dir string) (ToolMetadata, map[string]any, error) {
	meta, err := store.toolMetadata(ctx, "tflint")
	if err != nil {
		return meta, nil, err
	}
	cmd := exec.CommandContext(ctx, "tflint", "--format", "json")
	cmd.Dir = dir
	output, runErr := cmd.Output()
	var parsed struct {
		Issues []any `json:"issues"`
	}
	if len(output) > 0 {
		_ = json.Unmarshal(output, &parsed)
	}
	check := map[string]any{
		"tool":         "tflint",
		"check_type":   "terraform.lint",
		"issues_count": len(parsed.Issues),
	}
	if runErr != nil && len(output) == 0 {
		return meta, check, toolRunError{code: "tool_failed", message: "tflint failed", retryable: false}
	}
	return meta, check, nil
}

func runTrivy(ctx context.Context, store *Store, dir string) (ToolMetadata, []FindingUpsert, error) {
	meta, err := store.toolMetadata(ctx, "trivy")
	if err != nil {
		return meta, nil, err
	}
	cmd := exec.CommandContext(ctx, "trivy", "config", "--format", "json", "--skip-db-update", "--skip-policy-update", dir)
	output, runErr := cmd.Output()
	if runErr != nil && len(output) == 0 {
		return meta, nil, toolRunError{code: "tool_failed", message: "trivy config failed", retryable: false}
	}
	return meta, parseTrivyFindings(output, dir), nil
}

func (s *Store) toolMetadata(ctx context.Context, tool string) (ToolMetadata, error) {
	profile, err := s.ActiveToolProfile(ctx, tool)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ToolMetadata{Tool: tool, CompatibilityStatus: "unsupported", CertificationStatus: "uncertified"}, toolRunError{code: "tool_profile_unavailable", message: "active tool profile not found for " + tool, retryable: false}
		}
		return ToolMetadata{}, err
	}
	version, err := discoverToolVersion(ctx, tool)
	if err != nil {
		return ToolMetadata{Tool: tool, ProfileID: profile.ProfileID, ProfileVersion: profile.ProfileVersion, CompatibilityStatus: "unknown", CertificationStatus: "uncertified"}, err
	}
	meta := ToolMetadata{
		Tool:                tool,
		ToolVersion:         version,
		ProfileID:           profile.ProfileID,
		ProfileVersion:      profile.ProfileVersion,
		CompatibilityStatus: "compatible",
		CertificationStatus: "uncertified",
	}
	if versionMatches(profile.CertifiedVersions, version) {
		meta.CompatibilityStatus = "certified"
		meta.CertificationStatus = "certified"
		return meta, nil
	}
	if versionMatches(profile.CompatibleVersions, version) {
		return meta, nil
	}
	meta.CompatibilityStatus = "unsupported"
	return meta, toolRunError{code: "tool_version_unsupported", message: fmt.Sprintf("%s version %s is not certified by the active profile", tool, version), retryable: false}
}

func discoverToolVersion(ctx context.Context, tool string) (string, error) {
	cmd := exec.CommandContext(ctx, tool, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", toolRunError{code: "tool_unavailable", message: tool + " binary is unavailable", retryable: false}
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

func versionMatches(raw json.RawMessage, version string) bool {
	var prefixes []string
	if err := json.Unmarshal(raw, &prefixes); err != nil {
		return false
	}
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(version, prefix) {
			return true
		}
	}
	return false
}

func parseTrivyFindings(output []byte, projectDir string) []FindingUpsert {
	var payload struct {
		Results []struct {
			Target            string `json:"Target"`
			Misconfigurations []struct {
				ID            string `json:"ID"`
				Title         string `json:"Title"`
				Description   string `json:"Description"`
				Message       string `json:"Message"`
				Resolution    string `json:"Resolution"`
				Severity      string `json:"Severity"`
				PrimaryURL    string `json:"PrimaryURL"`
				CauseMetadata struct {
					Resource  string `json:"Resource"`
					Provider  string `json:"Provider"`
					Service   string `json:"Service"`
					StartLine int    `json:"StartLine"`
				} `json:"CauseMetadata"`
			} `json:"Misconfigurations"`
		} `json:"Results"`
	}
	if len(output) == 0 || json.Unmarshal(output, &payload) != nil {
		return nil
	}
	var out []FindingUpsert
	for _, result := range payload.Results {
		filePath := normalizeFindingPath(projectDir, result.Target)
		for _, misconfig := range result.Misconfigurations {
			resource := misconfig.CauseMetadata.Resource
			if resource == "" {
				resource = strings.Join([]string{misconfig.CauseMetadata.Provider, misconfig.CauseMetadata.Service}, ".")
				resource = strings.Trim(resource, ".")
			}
			title := nonEmpty(misconfig.Title, misconfig.Message)
			out = append(out, FindingUpsert{
				CheckType:     "terraform.security.misconfig",
				RuleID:        nonEmpty(misconfig.ID, "trivy.unknown"),
				Severity:      strings.ToLower(nonEmpty(misconfig.Severity, "info")),
				FilePath:      filePath,
				ResourceRef:   resource,
				Title:         nonEmpty(title, misconfig.ID),
				Description:   misconfig.Description,
				Remediation:   nonEmpty(misconfig.Resolution, misconfig.PrimaryURL),
				FindingKey:    fmt.Sprintf("%s:%s:%d", filePath, resource, misconfig.CauseMetadata.StartLine),
				RuleNamespace: "trivy",
			})
		}
	}
	return out
}

func normalizeFindingPath(projectDir, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "."
	}
	if filepath.IsAbs(target) {
		if rel, err := filepath.Rel(projectDir, target); err == nil {
			return cleanRelativePath(filepath.ToSlash(rel))
		}
	}
	return cleanRelativePath(filepath.ToSlash(target))
}
