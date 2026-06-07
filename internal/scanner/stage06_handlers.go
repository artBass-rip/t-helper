package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	providers, requiredAuth := detectTerraformProjectMetadata(project.Path)
	result.Providers = providers
	result.RequiredAuth = requiredAuth
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
	return raw, nil
}

var terraformProviderDeclRE = regexp.MustCompile(`(?m)^\s*provider\s+"([^"]+)"`)

func detectTerraformProjectMetadata(projectDir string) ([]string, []string) {
	seen := map[string]bool{}
	_ = filepath.WalkDir(projectDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			if entry != nil && entry.IsDir() && entry.Name() == ".terraform" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".tf") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, match := range terraformProviderDeclRE.FindAllStringSubmatch(string(raw), -1) {
			if len(match) == 2 && strings.TrimSpace(match[1]) != "" {
				seen[strings.TrimSpace(match[1])] = true
			}
		}
		return nil
	})
	providers := make([]string, 0, len(seen))
	requiredAuth := make([]string, 0, len(seen))
	for provider := range seen {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		requiredAuth = append(requiredAuth, "provider:"+provider)
	}
	return providers, requiredAuth
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
	result, err := runToolProfile(ctx, store, "terraform", dir)
	if err != nil {
		return result.Tool, nil, err
	}
	return result.Tool, result.Check, nil
}

func runTFLint(ctx context.Context, store *Store, dir string) (ToolMetadata, map[string]any, error) {
	result, err := runToolProfile(ctx, store, "tflint", dir)
	if err != nil {
		return result.Tool, nil, err
	}
	return result.Tool, result.Check, nil
}

func runTrivy(ctx context.Context, store *Store, dir string) (ToolMetadata, []FindingUpsert, error) {
	result, err := runToolProfile(ctx, store, "trivy", dir)
	if err != nil {
		return result.Tool, nil, err
	}
	return result.Tool, result.Findings, nil
}

func normalizeFindingPath(projectDir, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "."
	}
	if filepath.IsAbs(target) {
		if rel, err := filepath.Rel(projectDir, target); err == nil {
			return cleanFindingPath(filepath.ToSlash(rel))
		}
	}
	return cleanFindingPath(filepath.ToSlash(target))
}
