package scanner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
)

func (s *Store) GetProjectSettings(ctx context.Context, projectID string) (ProjectSettingsResponse, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return ProjectSettingsResponse{}, err
	}
	scan, err := s.ensureProjectScanSettings(ctx, projectID)
	if err != nil {
		return ProjectSettingsResponse{}, err
	}
	security, err := s.ensureProjectSecurityScanSettings(ctx, projectID)
	if err != nil {
		return ProjectSettingsResponse{}, err
	}
	return ProjectSettingsResponse{ProjectScanSettings: scan, Security: security}, nil
}

func (s *Store) UpsertProjectSettings(ctx context.Context, projectID string, input ProjectScanSettingsInput) (ProjectSettingsResponse, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return ProjectSettingsResponse{}, err
	}
	scan, err := s.ensureProjectScanSettings(ctx, projectID)
	if err != nil {
		return ProjectSettingsResponse{}, err
	}
	security, err := s.ensureProjectSecurityScanSettings(ctx, projectID)
	if err != nil {
		return ProjectSettingsResponse{}, err
	}
	if input.ScanEnabled != nil {
		scan.ScanEnabled = *input.ScanEnabled
	}
	if input.ScheduleEnabled != nil {
		scan.ScheduleEnabled = *input.ScheduleEnabled
	}
	if input.ScheduleFrequency != "" {
		scan.ScheduleFrequency = strings.TrimSpace(input.ScheduleFrequency)
	}
	if err := validateFrequency(scan.ScheduleFrequency); err != nil {
		return ProjectSettingsResponse{}, err
	}
	if input.RunAfterClone != nil {
		scan.RunAfterClone = *input.RunAfterClone
	}
	if input.RunAfterPull != nil {
		scan.RunAfterPull = *input.RunAfterPull
	}
	if strings.TrimSpace(input.ScanType) != "" {
		scan.ScanType = strings.TrimSpace(input.ScanType)
	}
	if !validScanType(scan.ScanType) {
		return ProjectSettingsResponse{}, validationErrorf("unsupported scan_type %q", scan.ScanType)
	}
	if input.Security != nil {
		if input.Security.Enabled != nil {
			security.Enabled = *input.Security.Enabled
		}
		if input.Security.EnabledModules != nil {
			modules, err := s.validateSecurityModules(ctx, input.Security.EnabledModules)
			if err != nil {
				return ProjectSettingsResponse{}, err
			}
			security.EnabledModules = modules
		}
		if input.Security.ScheduleEnabled != nil {
			security.ScheduleEnabled = *input.Security.ScheduleEnabled
		}
		if input.Security.ScheduleFrequency != "" {
			security.ScheduleFrequency = strings.TrimSpace(input.Security.ScheduleFrequency)
		}
		if err := validateFrequency(security.ScheduleFrequency); err != nil {
			return ProjectSettingsResponse{}, err
		}
		if input.Security.ValidateCode != nil {
			security.ValidateCode = *input.Security.ValidateCode
		}
	}
	now := time.Now().UTC()
	if err := s.updateProjectScanSettings(ctx, scan, now); err != nil {
		return ProjectSettingsResponse{}, err
	}
	if err := s.updateProjectSecurityScanSettings(ctx, security, now); err != nil {
		return ProjectSettingsResponse{}, err
	}
	return s.GetProjectSettings(ctx, projectID)
}

func (s *Store) ensureProjectScanSettings(ctx context.Context, projectID string) (ProjectScanSettings, error) {
	query := "SELECT id, project_id, scan_enabled, schedule_enabled, schedule_frequency, run_after_clone, run_after_pull, scan_type, created_at, updated_at FROM project_scan_settings WHERE project_id = ?"
	args := []any{projectID}
	if s.handle.Provider == "postgres" {
		query = strings.Replace(query, "?", "$1", 1)
	}
	item, err := scanProjectScanSettings(s.handle.DB.QueryRowContext(ctx, query, args...))
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProjectScanSettings{}, err
	}
	now := time.Now().UTC()
	item = ProjectScanSettings{ID: newID("project_scan_settings"), ProjectID: projectID, ScanEnabled: true, ScanType: ScanTypeTerraformFull, CreatedAt: now, UpdatedAt: now}
	query = `INSERT INTO project_scan_settings (id, project_id, scan_enabled, schedule_enabled, schedule_frequency, run_after_clone, run_after_pull, scan_type, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args = []any{item.ID, item.ProjectID, s.boolArg(item.ScanEnabled), s.boolArg(item.ScheduleEnabled), nullEmpty(item.ScheduleFrequency), s.boolArg(item.RunAfterClone), s.boolArg(item.RunAfterPull), item.ScanType, formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO project_scan_settings (id, project_id, scan_enabled, schedule_enabled, schedule_frequency, run_after_clone, run_after_pull, scan_type, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return ProjectScanSettings{}, err
	}
	return item, nil
}

func (s *Store) ensureProjectSecurityScanSettings(ctx context.Context, projectID string) (ProjectSecurityScanSettings, error) {
	query := "SELECT id, project_id, enabled, enabled_modules, schedule_enabled, schedule_frequency, validate_code, created_at, updated_at FROM project_security_scan_settings WHERE project_id = ?"
	args := []any{projectID}
	if s.handle.Provider == "postgres" {
		query = strings.Replace(query, "?", "$1", 1)
	}
	item, err := scanProjectSecurityScanSettings(s.handle.DB.QueryRowContext(ctx, query, args...))
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProjectSecurityScanSettings{}, err
	}
	now := time.Now().UTC()
	item = ProjectSecurityScanSettings{ID: newID("project_security_settings"), ProjectID: projectID, Enabled: true, EnabledModules: []string{DefaultSecurityModuleTrivy}, ValidateCode: true, CreatedAt: now, UpdatedAt: now}
	raw, _ := json.Marshal(item.EnabledModules)
	query = `INSERT INTO project_security_scan_settings (id, project_id, enabled, enabled_modules, schedule_enabled, schedule_frequency, validate_code, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args = []any{item.ID, item.ProjectID, s.boolArg(item.Enabled), string(raw), s.boolArg(item.ScheduleEnabled), nullEmpty(item.ScheduleFrequency), s.boolArg(item.ValidateCode), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO project_security_scan_settings (id, project_id, enabled, enabled_modules, schedule_enabled, schedule_frequency, validate_code, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return ProjectSecurityScanSettings{}, err
	}
	return item, nil
}

func (s *Store) updateProjectScanSettings(ctx context.Context, item ProjectScanSettings, now time.Time) error {
	query := `UPDATE project_scan_settings SET scan_enabled = ?, schedule_enabled = ?, schedule_frequency = ?, run_after_clone = ?, run_after_pull = ?, scan_type = ?, updated_at = ? WHERE project_id = ?`
	args := []any{s.boolArg(item.ScanEnabled), s.boolArg(item.ScheduleEnabled), nullEmpty(item.ScheduleFrequency), s.boolArg(item.RunAfterClone), s.boolArg(item.RunAfterPull), item.ScanType, formatTime(now), item.ProjectID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE project_scan_settings SET scan_enabled = $1, schedule_enabled = $2, schedule_frequency = $3, run_after_clone = $4, run_after_pull = $5, scan_type = $6, updated_at = $7 WHERE project_id = $8`
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) updateProjectSecurityScanSettings(ctx context.Context, item ProjectSecurityScanSettings, now time.Time) error {
	raw, _ := json.Marshal(item.EnabledModules)
	query := `UPDATE project_security_scan_settings SET enabled = ?, enabled_modules = ?, schedule_enabled = ?, schedule_frequency = ?, validate_code = ?, updated_at = ? WHERE project_id = ?`
	args := []any{s.boolArg(item.Enabled), string(raw), s.boolArg(item.ScheduleEnabled), nullEmpty(item.ScheduleFrequency), s.boolArg(item.ValidateCode), formatTime(now), item.ProjectID}
	if s.handle.Provider == "postgres" {
		query = `UPDATE project_security_scan_settings SET enabled = $1, enabled_modules = $2, schedule_enabled = $3, schedule_frequency = $4, validate_code = $5, updated_at = $6 WHERE project_id = $7`
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) CreateProjectScan(ctx context.Context, project Project, projectScanID, scanType, ruleSetID, jobID string) (ProjectScan, error) {
	if strings.TrimSpace(scanType) == "" {
		settings, err := s.ensureProjectScanSettings(ctx, project.ID)
		if err != nil {
			return ProjectScan{}, err
		}
		scanType = settings.ScanType
	}
	if !validScanType(scanType) {
		return ProjectScan{}, validationErrorf("unsupported scan_type %q", scanType)
	}
	if strings.TrimSpace(ruleSetID) == "" {
		ruleSet, err := s.ActiveRuleSet(ctx)
		if err == nil {
			ruleSetID = ruleSet.ID
		}
	}
	now := time.Now().UTC()
	if strings.TrimSpace(projectScanID) == "" {
		projectScanID = newID("project_scan")
	}
	item := ProjectScan{ID: projectScanID, JobID: jobID, ProjectID: project.ID, RuleSetID: ruleSetID, ScanType: scanType, Status: ProjectScanStatusQueued, CreatedAt: now, UpdatedAt: now}
	query := `INSERT INTO project_scans (id, job_id, project_id, rule_set_id, scan_type, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{item.ID, item.JobID, item.ProjectID, nullEmpty(item.RuleSetID), item.ScanType, item.Status, formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO project_scans (id, job_id, project_id, rule_set_id, scan_type, status, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			return s.GetProjectScanByJobID(ctx, jobID)
		}
		return ProjectScan{}, err
	}
	return item, nil
}

func (s *Store) GetProjectScanByJobID(ctx context.Context, jobID string) (ProjectScan, error) {
	query := "SELECT id, job_id, project_id, rule_set_id, scan_type, status, created_at, started_at, finished_at, result_payload, error_message, updated_at FROM project_scans WHERE job_id = ?"
	args := []any{jobID}
	if s.handle.Provider == "postgres" {
		query = strings.Replace(query, "?", "$1", 1)
	}
	item, err := scanProjectScan(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectScan{}, ErrNotFound
	}
	return item, err
}

func (s *Store) GetProjectScan(ctx context.Context, id string) (ProjectScan, error) {
	query := "SELECT id, job_id, project_id, rule_set_id, scan_type, status, created_at, started_at, finished_at, result_payload, error_message, updated_at FROM project_scans WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = strings.Replace(query, "?", "$1", 1)
	}
	item, err := scanProjectScan(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectScan{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListProjectScans(ctx context.Context, opts ProjectScanListOptions) (Page[ProjectScan], error) {
	query := "SELECT id, job_id, project_id, rule_set_id, scan_type, status, created_at, started_at, finished_at, result_payload, error_message, updated_at FROM project_scans"
	var args []any
	var where []string
	add := func(clause, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, value)
			where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
		}
	}
	add("project_id = %s", opts.ProjectID)
	add("status = %s", opts.Status)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanProjectScan)
}

func (s *Store) MarkProjectScanRunning(ctx context.Context, projectScanID string) error {
	now := time.Now().UTC()
	query := "UPDATE project_scans SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ? WHERE id = ?"
	args := []any{ProjectScanStatusRunning, formatTime(now), formatTime(now), projectScanID}
	if s.handle.Provider == "postgres" {
		query = "UPDATE project_scans SET status = $1, started_at = COALESCE(started_at, $2), updated_at = $3 WHERE id = $4"
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) UpdateProjectScanAggregate(ctx context.Context, projectScanID, status string, result json.RawMessage, errorMessage string) error {
	now := time.Now().UTC()
	query := "UPDATE project_scans SET status = ?, finished_at = ?, result_payload = ?, error_message = ?, updated_at = ? WHERE id = ?"
	args := []any{status, formatTime(now), string(result), nullEmpty(errorMessage), formatTime(now), projectScanID}
	if status == ProjectScanStatusRunning || status == ProjectScanStatusQueued {
		query = "UPDATE project_scans SET status = ?, result_payload = ?, error_message = ?, updated_at = ? WHERE id = ?"
		args = []any{status, string(result), nullEmpty(errorMessage), formatTime(now), projectScanID}
	}
	if s.handle.Provider == "postgres" {
		if status == ProjectScanStatusRunning || status == ProjectScanStatusQueued {
			query = "UPDATE project_scans SET status = $1, result_payload = $2, error_message = $3, updated_at = $4 WHERE id = $5"
		} else {
			query = "UPDATE project_scans SET status = $1, finished_at = $2, result_payload = $3, error_message = $4, updated_at = $5 WHERE id = $6"
		}
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ActiveToolProfile(ctx context.Context, tool string) (ToolProfile, error) {
	query := "SELECT id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at FROM tool_profiles WHERE tool = ? AND active = ? ORDER BY updated_at DESC, id DESC LIMIT 1"
	args := []any{tool, s.boolArg(true)}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at FROM tool_profiles WHERE tool = $1 AND active = $2 ORDER BY updated_at DESC, id DESC LIMIT 1"
	}
	item, err := scanToolProfile(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ToolProfile{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListToolProfiles(ctx context.Context, opts ToolProfileListOptions) (Page[ToolProfile], error) {
	query := "SELECT id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at FROM tool_profiles"
	var args []any
	var where []string
	add := func(clause string, value any, enabled bool) {
		if enabled {
			args = append(args, value)
			where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
		}
	}
	add("tool = %s", opts.Tool, strings.TrimSpace(opts.Tool) != "")
	add("source_type = %s", opts.SourceType, strings.TrimSpace(opts.SourceType) != "")
	if opts.Active != nil {
		add("active = %s", s.boolArg(*opts.Active), true)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanToolProfile)
}

func (s *Store) ValidateToolProfile(ctx context.Context, input ToolProfileValidateInput) (ToolProfileValidationResult, error) {
	raw, sourcePath, err := readToolProfileInput(input.ProfilePath, input.ProfilePayload)
	status := "passed"
	diagnostics := map[string]any{}
	var doc toolProfileDocument
	if err != nil {
		status = "failed"
		diagnostics["error"] = redactSensitiveText(err.Error())
	} else {
		doc, err = decodeToolProfile(raw)
		if err != nil {
			status = "failed"
			diagnostics["error"] = redactSensitiveText(err.Error())
		} else if strings.TrimSpace(input.FixtureSet) != "" {
			if err := validateToolProfileFixtureSet(doc, input.FixtureSet); err != nil {
				status = "failed"
				diagnostics["error"] = redactSensitiveText(err.Error())
			} else {
				diagnostics["fixture_set"] = redactSensitiveText(strings.TrimSpace(input.FixtureSet))
			}
		}
	}
	if sourcePath != "" {
		diagnostics["profile_path"] = redactSensitiveText(sourcePath)
	}
	if doc.Tool == "" || !allowedTool(doc.Tool) {
		doc.Tool = "unknown"
	}
	return s.insertToolProfileValidationResult(ctx, doc, "", strings.TrimSpace(input.FixtureSet), status, diagnostics)
}

func (s *Store) ImportToolProfile(ctx context.Context, input ToolProfileImportInput) (ToolProfile, error) {
	raw, sourcePath, err := readToolProfileInput(input.ProfilePath, input.ProfilePayload)
	if err != nil {
		return ToolProfile{}, err
	}
	doc, err := decodeToolProfile(raw)
	if err != nil {
		return ToolProfile{}, err
	}
	sourceType := "local_upload"
	if sourcePath != "" {
		sourceType = "local_path"
		if strings.HasPrefix(filepath.ToSlash(sourcePath), "tool-profiles/") {
			sourceType = "bundled"
		}
	} else {
		sourcePath, err = materializeUploadedToolProfile(raw, doc)
		if err != nil {
			return ToolProfile{}, err
		}
	}
	now := time.Now().UTC()
	id := newID("tool_profile")
	certified, _ := json.Marshal(doc.CertifiedVersions)
	compatible, _ := json.Marshal(doc.CompatibleVersions)
	query := `INSERT INTO tool_profiles (id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (tool, profile_id, profile_version) DO UPDATE SET schema_version = excluded.schema_version, source_type = excluded.source_type, source_path = excluded.source_path, checksum = excluded.checksum, certified_versions = excluded.certified_versions, compatible_versions = excluded.compatible_versions, updated_at = excluded.updated_at`
	args := []any{id, doc.Tool, doc.ProfileID, doc.ProfileVersion, doc.SchemaVersion, sourceType, nullEmpty(sourcePath), profileChecksum(raw), string(certified), string(compatible), s.boolArg(false), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO tool_profiles (id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (tool, profile_id, profile_version) DO UPDATE SET schema_version = EXCLUDED.schema_version, source_type = EXCLUDED.source_type, source_path = EXCLUDED.source_path, checksum = EXCLUDED.checksum, certified_versions = EXCLUDED.certified_versions, compatible_versions = EXCLUDED.compatible_versions, updated_at = EXCLUDED.updated_at`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return ToolProfile{}, err
	}
	return s.toolProfileByIdentity(ctx, doc.Tool, doc.ProfileID, doc.ProfileVersion)
}

func materializeUploadedToolProfile(raw []byte, doc toolProfileDocument) (string, error) {
	fileName := safeToolProfileFileName(doc.ProfileID + "-" + doc.ProfileVersion + ".json")
	path := workspaceArtifactPath(filepath.Join("tool-profiles", doc.Tool, fileName))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func workspaceArtifactPath(relative string) string {
	dir, err := os.Getwd()
	if err != nil {
		return filepath.Join(".artifacts", relative)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, ".artifacts", relative)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(".artifacts", relative)
		}
		dir = parent
	}
}

func safeToolProfileFileName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "profile.json"
	}
	return b.String()
}

func (s *Store) ActivateToolProfile(ctx context.Context, input ToolProfileActivateInput) (ToolProfile, error) {
	tool := strings.TrimSpace(input.Tool)
	profileID := strings.TrimSpace(input.ProfileID)
	profileVersion := strings.TrimSpace(input.ProfileVersion)
	if tool == "" || profileID == "" || profileVersion == "" {
		return ToolProfile{}, validationErrorf("tool, profile_id and profile_version are required")
	}
	item, err := s.toolProfileByIdentity(ctx, tool, profileID, profileVersion)
	if err != nil {
		return ToolProfile{}, err
	}
	if item.SourceType == "generated_candidate" {
		passed, err := s.toolProfileHasPassedValidation(ctx, item)
		if err != nil {
			return ToolProfile{}, err
		}
		if !passed {
			return ToolProfile{}, validationErrorf("generated_candidate profile requires passed validation before activation")
		}
	}
	now := time.Now().UTC()
	clearQuery := "UPDATE tool_profiles SET active = ?, updated_at = ? WHERE tool = ?"
	setQuery := "UPDATE tool_profiles SET active = ?, updated_at = ? WHERE tool = ? AND profile_id = ? AND profile_version = ?"
	argsClear := []any{s.boolArg(false), formatTime(now), tool}
	argsSet := []any{s.boolArg(true), formatTime(now), tool, profileID, profileVersion}
	if s.handle.Provider == "postgres" {
		clearQuery = "UPDATE tool_profiles SET active = $1, updated_at = $2 WHERE tool = $3"
		setQuery = "UPDATE tool_profiles SET active = $1, updated_at = $2 WHERE tool = $3 AND profile_id = $4 AND profile_version = $5"
	}
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return ToolProfile{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, clearQuery, argsClear...); err != nil {
		return ToolProfile{}, err
	}
	if _, err := tx.ExecContext(ctx, setQuery, argsSet...); err != nil {
		return ToolProfile{}, err
	}
	if err := tx.Commit(); err != nil {
		return ToolProfile{}, err
	}
	return s.toolProfileByIdentity(ctx, tool, profileID, profileVersion)
}

func (s *Store) AnalyzeToolProfile(ctx context.Context, input ToolProfileAnalyzeInput) (ToolProfileCandidate, error) {
	diagnostics, _ := json.Marshal(map[string]any{
		"message":      "automatic profile analyzer is not enabled in this MVP build",
		"samples_path": redactSensitiveText(strings.TrimSpace(input.SamplesPath)),
	})
	return ToolProfileCandidate{
		SchemaVersion:     toolProfileCandidateVersion,
		BaselineProfileID: strings.TrimSpace(input.BaselineProfileID),
		Confidence:        "unsupported",
		Diagnostics:       json.RawMessage(diagnostics),
	}, nil
}

func (s *Store) toolProfileByIdentity(ctx context.Context, tool, profileID, profileVersion string) (ToolProfile, error) {
	query := "SELECT id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at FROM tool_profiles WHERE tool = ? AND profile_id = ? AND profile_version = ?"
	args := []any{tool, profileID, profileVersion}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, tool, profile_id, profile_version, schema_version, source_type, source_path, checksum, certified_versions, compatible_versions, active, created_at, updated_at FROM tool_profiles WHERE tool = $1 AND profile_id = $2 AND profile_version = $3"
	}
	item, err := scanToolProfile(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ToolProfile{}, ErrNotFound
	}
	return item, err
}

func (s *Store) toolProfileHasPassedValidation(ctx context.Context, profile ToolProfile) (bool, error) {
	query := "SELECT COUNT(*) FROM tool_profile_validation_results WHERE tool_profile_id = ? AND validation_status = 'passed'"
	args := []any{profile.ID}
	if s.handle.Provider == "postgres" {
		query = "SELECT COUNT(*) FROM tool_profile_validation_results WHERE tool_profile_id = $1 AND validation_status = 'passed'"
	}
	var count int
	if err := s.handle.DB.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) insertToolProfileValidationResult(ctx context.Context, doc toolProfileDocument, profileID, fixtureSet, status string, diagnostics map[string]any) (ToolProfileValidationResult, error) {
	now := time.Now().UTC()
	rawDiagnostics, _ := json.Marshal(diagnostics)
	id := newID("tool_profile_validation")
	tool := doc.Tool
	query := `INSERT INTO tool_profile_validation_results (id, tool_profile_id, tool, tool_version, fixture_set, validation_status, diagnostics, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{id, nullEmpty(profileID), tool, nil, nullEmpty(fixtureSet), status, string(rawDiagnostics), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO tool_profile_validation_results (id, tool_profile_id, tool, tool_version, fixture_set, validation_status, diagnostics, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return ToolProfileValidationResult{}, err
	}
	return ToolProfileValidationResult{ID: id, ToolProfileID: profileID, Tool: tool, FixtureSet: fixtureSet, ValidationStatus: status, Diagnostics: json.RawMessage(rawDiagnostics), CreatedAt: now}, nil
}

func readToolProfileInput(path string, payload json.RawMessage) ([]byte, string, error) {
	path = strings.TrimSpace(path)
	if len(payload) > 0 && strings.TrimSpace(string(payload)) != "" {
		if !json.Valid(payload) {
			return nil, "", validationErrorf("profile_payload must be valid JSON")
		}
		return []byte(payload), "", nil
	}
	if path == "" {
		return nil, "", validationErrorf("profile_path or profile_payload is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	return raw, path, nil
}

func (s *Store) ActiveRuleSet(ctx context.Context) (SecurityRuleSet, error) {
	query := "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets WHERE active = ? ORDER BY updated_at DESC, id DESC LIMIT 1"
	args := []any{s.boolArg(true)}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets WHERE active = $1 ORDER BY updated_at DESC, id DESC LIMIT 1"
	}
	item, err := scanRuleSet(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return SecurityRuleSet{}, ErrNotFound
	}
	return item, err
}

func (s *Store) GetRuleSet(ctx context.Context, id string) (SecurityRuleSet, error) {
	query := "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets WHERE id = $1"
	}
	item, err := scanRuleSet(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return SecurityRuleSet{}, ErrNotFound
	}
	return item, err
}

func (s *Store) UpsertRuleSet(ctx context.Context, input SecurityRuleSetInput) (SecurityRuleSet, error) {
	name := strings.TrimSpace(input.Name)
	version := strings.TrimSpace(input.Version)
	if name == "" || version == "" {
		return SecurityRuleSet{}, validationErrorf("name and version are required")
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "bundled"
	}
	if sourceType != "bundled" && sourceType != "local_upload" && sourceType != "local_path" {
		return SecurityRuleSet{}, validationErrorf("unsupported source_type %q", sourceType)
	}
	active := false
	if input.Active != nil {
		active = *input.Active
	}
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = newID("security_rule_set")
	}
	query := `INSERT INTO security_rule_sets (id, name, version, source_type, checksum, active, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name, version) DO UPDATE SET source_type = excluded.source_type, checksum = excluded.checksum, active = excluded.active, updated_at = excluded.updated_at`
	args := []any{id, name, version, sourceType, nullEmpty(input.Checksum), s.boolArg(active), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO security_rule_sets (id, name, version, source_type, checksum, active, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (name, version) DO UPDATE SET source_type = EXCLUDED.source_type, checksum = EXCLUDED.checksum, active = EXCLUDED.active, updated_at = EXCLUDED.updated_at`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return SecurityRuleSet{}, err
	}
	return s.ruleSetByNameVersion(ctx, name, version)
}

func (s *Store) ruleSetByNameVersion(ctx context.Context, name, version string) (SecurityRuleSet, error) {
	query := "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets WHERE name = ? AND version = ?"
	args := []any{name, version}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets WHERE name = $1 AND version = $2"
	}
	return scanRuleSet(s.handle.DB.QueryRowContext(ctx, query, args...))
}

func (s *Store) ListRuleSets(ctx context.Context, opts RuleSetListOptions) (Page[SecurityRuleSet], error) {
	query := "SELECT id, name, version, source_type, checksum, active, created_at, updated_at FROM security_rule_sets"
	var args []any
	if opts.Active != nil {
		args = append(args, s.boolArg(*opts.Active))
		query += " WHERE active = " + s.placeholder(len(args))
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanRuleSet)
}

func (s *Store) UpsertFinding(ctx context.Context, input FindingUpsert) (FindingUpsertResult, error) {
	if strings.TrimSpace(input.RuleID) == "" {
		input.RuleID = "unknown"
	}
	input.RuleID = redactSensitiveText(input.RuleID)
	if strings.TrimSpace(input.Severity) == "" {
		input.Severity = "info"
	}
	if !validSeverity(input.Severity) {
		input.Severity = "info"
	}
	if strings.TrimSpace(input.Title) == "" {
		input.Title = input.RuleID
	}
	input.Title = redactSensitiveText(input.Title)
	input.Description = redactSensitiveText(input.Description)
	input.Remediation = redactSensitiveText(input.Remediation)
	input.RuleNamespace = redactSensitiveText(input.RuleNamespace)
	input.Tool = redactSensitiveText(input.Tool)
	input.CheckType = redactSensitiveText(input.CheckType)
	stableFindingKey := strings.TrimSpace(redactSensitiveText(input.FindingKey))
	resourceRef := strings.TrimSpace(redactSensitiveText(input.ResourceRef))
	if stableFindingKey == "" && resourceRef == "" {
		return FindingUpsertResult{}, validationErrorf("security finding requires resource_ref or stable finding_key")
	}
	if stableFindingKey == "" {
		stableFindingKey = resourceRef
	}
	components := map[string]any{
		"schema_version":       FindingFingerprintSchema,
		"project_id":           nullStringValue(input.ProjectID),
		"workspace_id":         nullStringValue(input.WorkspaceID),
		"rule_set_id":          nullStringValue(input.RuleSetID),
		"rule_namespace":       nonEmpty(input.RuleNamespace, input.Tool),
		"tool":                 input.Tool,
		"check_type":           redactSensitiveText(input.CheckType),
		"rule_id":              input.RuleID,
		"normalized_file_path": cleanRelativePath(input.FilePath),
		"resource_ref":         nullStringValue(resourceRef),
		"finding_key":          stableFindingKey,
	}
	rawComponents, _ := json.Marshal(components)
	sum := sha256.Sum256(rawComponents)
	fingerprint := "fp:v1:" + hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	id := newID("security_finding")
	query := `INSERT INTO security_findings (id, project_id, repository_id, workspace_id, job_id, rule_set_id, check_type, rule_id, severity, status, file_path, resource_ref, title, description, remediation, fingerprint, fingerprint_schema_version, fingerprint_components, first_seen_at, last_seen_at, detected_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (fingerprint) DO UPDATE SET job_id = excluded.job_id, severity = excluded.severity, status = CASE WHEN security_findings.status = 'fixed' THEN 'open' ELSE security_findings.status END, title = excluded.title, description = excluded.description, remediation = excluded.remediation, last_seen_at = excluded.last_seen_at, detected_at = excluded.detected_at, updated_at = excluded.updated_at`
	args := []any{id, nullEmpty(input.ProjectID), nullEmpty(input.RepositoryID), nullEmpty(input.WorkspaceID), nullEmpty(input.JobID), nullEmpty(input.RuleSetID), nonEmpty(input.CheckType, "terraform.security.misconfig"), input.RuleID, input.Severity, FindingStatusOpen, nullEmpty(cleanRelativePath(input.FilePath)), nullEmpty(resourceRef), input.Title, nullEmpty(input.Description), nullEmpty(input.Remediation), fingerprint, FindingFingerprintSchema, string(rawComponents), formatTime(now), formatTime(now), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO security_findings (id, project_id, repository_id, workspace_id, job_id, rule_set_id, check_type, rule_id, severity, status, file_path, resource_ref, title, description, remediation, fingerprint, fingerprint_schema_version, fingerprint_components, first_seen_at, last_seen_at, detected_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
ON CONFLICT (fingerprint) DO UPDATE SET job_id = EXCLUDED.job_id, severity = EXCLUDED.severity, status = CASE WHEN security_findings.status = 'fixed' THEN 'open' ELSE security_findings.status END, title = EXCLUDED.title, description = EXCLUDED.description, remediation = EXCLUDED.remediation, last_seen_at = EXCLUDED.last_seen_at, detected_at = EXCLUDED.detected_at, updated_at = EXCLUDED.updated_at`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return FindingUpsertResult{}, err
	}
	finding, err := s.findingByFingerprint(ctx, fingerprint)
	if err != nil {
		return FindingUpsertResult{}, err
	}
	return FindingUpsertResult{Created: finding.ID == id, Finding: finding}, nil
}

func (s *Store) GetFinding(ctx context.Context, id string) (SecurityFinding, error) {
	query := s.findingSelect() + " WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = s.findingSelect() + " WHERE id = $1"
	}
	item, err := scanFinding(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return SecurityFinding{}, ErrNotFound
	}
	return item, err
}

func (s *Store) findingByFingerprint(ctx context.Context, fingerprint string) (SecurityFinding, error) {
	query := s.findingSelect() + " WHERE fingerprint = ?"
	args := []any{fingerprint}
	if s.handle.Provider == "postgres" {
		query = s.findingSelect() + " WHERE fingerprint = $1"
	}
	return scanFinding(s.handle.DB.QueryRowContext(ctx, query, args...))
}

func (s *Store) ListFindings(ctx context.Context, opts FindingListOptions) (Page[SecurityFinding], error) {
	query := s.findingSelect()
	var args []any
	var where []string
	add := func(clause, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, value)
			where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
		}
	}
	add("project_id = %s", opts.ProjectID)
	add("repository_id = %s", opts.RepositoryID)
	add("severity = %s", opts.Severity)
	add("status = %s", opts.Status)
	if opts.ProjectScanID != "" {
		scan, err := s.GetProjectScan(ctx, opts.ProjectScanID)
		if err != nil {
			return Page[SecurityFinding]{}, err
		}
		add("project_id = %s", scan.ProjectID)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "updated_at", opts.ListOptions, scanFinding)
}

func (s *Store) validateSecurityModules(ctx context.Context, modules []string) ([]string, error) {
	allowed, err := s.ConfigSecurityModules(ctx)
	if err != nil {
		return nil, err
	}
	allowedSet := map[string]bool{}
	for _, module := range allowed {
		allowedSet[module] = true
	}
	if len(allowedSet) == 0 {
		allowedSet[DefaultSecurityModuleTrivy] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, module := range modules {
		module = strings.TrimSpace(module)
		if module == "" {
			return nil, validationErrorf("security.enabled_modules contains empty module")
		}
		if !allowedSet[module] {
			return nil, validationErrorf("unsupported security module %q", module)
		}
		if !seen[module] {
			seen[module] = true
			out = append(out, module)
		}
	}
	return out, nil
}

func (s *Store) ConfigSecurityModules(ctx context.Context) ([]string, error) {
	query := "SELECT value FROM config_entries WHERE scope = ? AND key = ?"
	args := []any{"system", "scanning.security_scan"}
	if s.handle.Provider == "postgres" {
		query = "SELECT value FROM config_entries WHERE scope = $1 AND key = $2"
	}
	var raw string
	if err := s.handle.DB.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []string{DefaultSecurityModuleTrivy}, nil
		}
		return nil, err
	}
	var cfg struct {
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	if len(cfg.Modules) == 0 {
		return []string{DefaultSecurityModuleTrivy}, nil
	}
	return cfg.Modules, nil
}

func (s *Store) ApplyWorkflowAggregateToProjectScan(ctx context.Context, jobGroupID string) error {
	workflow, err := jobs.NewStore(s.handle).WorkflowStatus(ctx, jobGroupID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			return nil
		}
		return err
	}
	projectScanID := workflow.WorkflowID
	if !strings.HasPrefix(jobGroupID, "project_scan:") || projectScanID == "" {
		return nil
	}
	projectScan, err := s.GetProjectScan(ctx, projectScanID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	findings, err := s.findingSeveritySummary(ctx, projectScan.ProjectID)
	if err != nil {
		return err
	}
	children, err := s.projectScanChildJobs(ctx, jobGroupID)
	if err != nil {
		return err
	}
	tools, providers, requiredAuth, checkResults, err := s.projectScanAggregateParts(ctx, jobGroupID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"schema_version":   ProjectScanAggregateSchema,
		"job_group_id":     jobGroupID,
		"parent_job_id":    projectScan.JobID,
		"child_job_ids":    children,
		"tools":            tools,
		"providers":        providers,
		"required_auth":    requiredAuth,
		"check_results":    checkResults,
		"findings_summary": findings,
	})
	return s.UpdateProjectScanAggregate(ctx, projectScanID, workflow.AggregateStatus, payload, "")
}

func (s *Store) projectScanAggregateParts(ctx context.Context, jobGroupID string) ([]ToolMetadata, []string, []string, []any, error) {
	query := "SELECT job_type, result_payload FROM jobs WHERE job_group_id = ? AND result_payload IS NOT NULL ORDER BY created_at ASC, id ASC"
	args := []any{jobGroupID}
	if s.handle.Provider == "postgres" {
		query = "SELECT job_type, result_payload FROM jobs WHERE job_group_id = $1 AND result_payload IS NOT NULL ORDER BY created_at ASC, id ASC"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer rows.Close()
	var tools []ToolMetadata
	var providers []string
	var requiredAuth []string
	var checkResults []any
	seenTools := map[string]bool{}
	appendTools := func(items []ToolMetadata) {
		for _, item := range items {
			key := item.Tool + ":" + item.ToolVersion + ":" + item.ProfileID + ":" + item.ProfileVersion
			if !seenTools[key] {
				tools = append(tools, item)
				seenTools[key] = true
			}
		}
	}
	for rows.Next() {
		var jobType string
		var raw string
		if err := rows.Scan(&jobType, &raw); err != nil {
			return nil, nil, nil, nil, err
		}
		switch jobType {
		case "project_scan":
			var result ProjectScanResult
			if json.Unmarshal([]byte(raw), &result) == nil && result.SchemaVersion == ProjectScanResultSchema {
				appendTools(result.Tools)
				providers = appendStringSet(providers, result.Providers...)
				requiredAuth = appendStringSet(requiredAuth, result.RequiredAuth...)
				checkResults = append(checkResults, result.CheckResults...)
			}
		case "security_validation_scan":
			var result SecurityScanResult
			if json.Unmarshal([]byte(raw), &result) == nil && result.SchemaVersion == SecurityScanResultSchema {
				appendTools(result.Tools)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	if tools == nil {
		tools = []ToolMetadata{}
	}
	if providers == nil {
		providers = []string{}
	}
	if requiredAuth == nil {
		requiredAuth = []string{}
	}
	if checkResults == nil {
		checkResults = []any{}
	}
	return tools, providers, requiredAuth, checkResults, nil
}

func appendStringSet(existing []string, values ...string) []string {
	seen := make(map[string]bool, len(existing)+len(values))
	for _, value := range existing {
		seen[value] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			existing = append(existing, value)
			seen[value] = true
		}
	}
	return existing
}

func (s *Store) findingSeveritySummary(ctx context.Context, projectID string) (map[string]int, error) {
	out := map[string]int{"info": 0, "low": 0, "medium": 0, "high": 0, "critical": 0}
	query := "SELECT severity, count(*) FROM security_findings WHERE project_id = ? AND status = 'open' GROUP BY severity"
	args := []any{projectID}
	if s.handle.Provider == "postgres" {
		query = "SELECT severity, count(*) FROM security_findings WHERE project_id = $1 AND status = 'open' GROUP BY severity"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var severity string
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			return nil, err
		}
		out[severity] = count
	}
	return out, rows.Err()
}

func (s *Store) projectScanChildJobs(ctx context.Context, jobGroupID string) ([]string, error) {
	query := "SELECT id FROM jobs WHERE job_group_id = ? AND parent_job_id IS NOT NULL AND parent_job_id <> '' ORDER BY created_at ASC, id ASC"
	args := []any{jobGroupID}
	if s.handle.Provider == "postgres" {
		query = "SELECT id FROM jobs WHERE job_group_id = $1 AND parent_job_id IS NOT NULL AND parent_job_id <> '' ORDER BY created_at ASC, id ASC"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) findingSelect() string {
	return "SELECT id, project_id, repository_id, workspace_id, job_id, rule_set_id, check_type, rule_id, severity, status, file_path, resource_ref, title, description, remediation, fingerprint, fingerprint_schema_version, fingerprint_components, first_seen_at, last_seen_at, detected_at, updated_at FROM security_findings"
}

func validScanType(value string) bool {
	switch value {
	case ScanTypeTerraformStatic, ScanTypeTerraformValidate, ScanTypeTerraformSecurity, ScanTypeTerraformFull, ScanTypeSecurityValidation:
		return true
	default:
		return false
	}
}

func validSeverity(value string) bool {
	switch strings.ToLower(value) {
	case "info", "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func nullStringValue(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func scanProjectScanSettings(row interface{ Scan(dest ...any) error }) (ProjectScanSettings, error) {
	var item ProjectScanSettings
	var frequency sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.ScanEnabled, &item.ScheduleEnabled, &frequency, &item.RunAfterClone, &item.RunAfterPull, &item.ScanType, &created, &updated); err != nil {
		return item, err
	}
	item.ScheduleFrequency = frequency.String
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanProjectSecurityScanSettings(row interface{ Scan(dest ...any) error }) (ProjectSecurityScanSettings, error) {
	var item ProjectSecurityScanSettings
	var modules string
	var frequency sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.Enabled, &modules, &item.ScheduleEnabled, &frequency, &item.ValidateCode, &created, &updated); err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(modules), &item.EnabledModules)
	item.ScheduleFrequency = frequency.String
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanProjectScan(row interface{ Scan(dest ...any) error }) (ProjectScan, error) {
	var item ProjectScan
	var ruleSetID, result, errorMessage, started, finished sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.JobID, &item.ProjectID, &ruleSetID, &item.ScanType, &item.Status, &created, &started, &finished, &result, &errorMessage, &updated); err != nil {
		return item, err
	}
	item.RuleSetID = ruleSetID.String
	if result.Valid {
		item.ResultPayload = json.RawMessage(result.String)
	}
	item.ErrorMessage = errorMessage.String
	item.CreatedAt, _ = parseTime(created)
	item.StartedAt = parseTimePtr(started.String)
	item.FinishedAt = parseTimePtr(finished.String)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanToolProfile(row interface{ Scan(dest ...any) error }) (ToolProfile, error) {
	var item ToolProfile
	var sourcePath, checksum sql.NullString
	var certified, compatible, created, updated string
	if err := row.Scan(&item.ID, &item.Tool, &item.ProfileID, &item.ProfileVersion, &item.SchemaVersion, &item.SourceType, &sourcePath, &checksum, &certified, &compatible, &item.Active, &created, &updated); err != nil {
		return item, err
	}
	item.SourcePath = sourcePath.String
	item.Checksum = checksum.String
	item.CertifiedVersions = json.RawMessage(certified)
	item.CompatibleVersions = json.RawMessage(compatible)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanRuleSet(row interface{ Scan(dest ...any) error }) (SecurityRuleSet, error) {
	var item SecurityRuleSet
	var checksum sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.Version, &item.SourceType, &checksum, &item.Active, &created, &updated); err != nil {
		return item, err
	}
	item.Checksum = checksum.String
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanFinding(row interface{ Scan(dest ...any) error }) (SecurityFinding, error) {
	var item SecurityFinding
	var projectID, repositoryID, workspaceID, jobID, ruleSetID, filePath, resourceRef, description, remediation sql.NullString
	var components string
	var firstSeen, lastSeen, detected, updated string
	if err := row.Scan(&item.ID, &projectID, &repositoryID, &workspaceID, &jobID, &ruleSetID, &item.CheckType, &item.RuleID, &item.Severity, &item.Status, &filePath, &resourceRef, &item.Title, &description, &remediation, &item.Fingerprint, &item.FingerprintSchemaVersion, &components, &firstSeen, &lastSeen, &detected, &updated); err != nil {
		return item, err
	}
	item.ProjectID = projectID.String
	item.RepositoryID = repositoryID.String
	item.WorkspaceID = workspaceID.String
	item.JobID = jobID.String
	item.RuleSetID = ruleSetID.String
	item.FilePath = filePath.String
	item.ResourceRef = resourceRef.String
	item.Description = description.String
	item.Remediation = remediation.String
	item.FingerprintComponents = json.RawMessage(components)
	item.FirstSeenAt, _ = parseTime(firstSeen)
	item.LastSeenAt, _ = parseTime(lastSeen)
	item.DetectedAt, _ = parseTime(detected)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}
