package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/scanner"
	"github.com/go-chi/chi/v5"
)

const idempotencyKeyHeader = "Idempotency-Key"

type ScannerHandler struct {
	store    *scanner.Store
	jobStore *jobs.Store
}

func NewScannerHandler(store *scanner.Store, jobStore *jobs.Store) *ScannerHandler {
	return &ScannerHandler{store: store, jobStore: jobStore}
}

func (h *ScannerHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/root-paths", h.ListRootPaths)
	r.Put("/api/root-paths", h.PutRootPaths)
	r.Post("/api/scans", h.CreateScan)
	r.Get("/api/scans/{job_id}", h.GetScan)
	r.Get("/api/projects", h.ListProjects)
	r.Get("/api/projects/{id}", h.GetProject)
	r.Get("/api/projects/{id}/links", h.ListProjectLinks)
	r.Get("/api/projects/{id}/scan-settings", h.GetProjectScanSettings)
	r.Put("/api/projects/{id}/scan-settings", h.PutProjectScanSettings)
	r.Post("/api/project-scans", h.CreateProjectScan)
	r.Get("/api/project-scans", h.ListProjectScans)
	r.Get("/api/project-scans/{project_scan_id}", h.GetProjectScan)
	r.Get("/api/project-scans/{project_scan_id}/findings", h.ListProjectScanFindings)
	r.Get("/api/repos", h.ListRepositories)
	r.Get("/api/repos/{id}", h.GetRepository)
	r.Get("/api/ignore-rules", h.ListIgnoreRules)
	r.Put("/api/ignore-rules", h.PutIgnoreRules)
	r.Get("/api/environments", h.ListEnvironments)
	r.Get("/api/environments/{id}", h.GetEnvironment)
	r.Get("/api/workspaces", h.ListWorkspaces)
	r.Get("/api/workspaces/{id}", h.GetWorkspace)
	r.Get("/api/security/findings", h.ListSecurityFindings)
	r.Get("/api/security/findings/{id}", h.GetSecurityFinding)
	r.Get("/api/security/rule-sets", h.ListSecurityRuleSets)
	r.Put("/api/security/rule-sets", h.PutSecurityRuleSet)
	r.Get("/api/tool-profiles", h.ListToolProfiles)
}

func (h *ScannerHandler) ListRootPaths(w http.ResponseWriter, r *http.Request) {
	if err := h.store.SyncRootPathsFromConfig(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListRootPaths(r.Context(), scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) PutRootPaths(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RootPaths []scanner.RootPathInput `json:"root_paths"`
	}
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	for idx := range req.RootPaths {
		if req.RootPaths[idx].Source != "" {
			writeError(w, r, http.StatusBadRequest, "validation_error", "root_paths.source is read-only")
			return
		}
		req.RootPaths[idx].Source = scanner.RootPathSourceAPI
	}
	items, err := h.store.UpsertRootPaths(r.Context(), req.RootPaths)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (h *ScannerHandler) CreateScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RootPathIDs []string `json:"root_path_ids"`
		Reason      string   `json:"reason"`
	}
	if err := decodeStrictJSON(r, &req, true); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if err := h.store.SyncRootPathsFromConfig(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	roots, err := h.scanRoots(r, req.RootPathIDs)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if len(roots) == 0 {
		writeError(w, r, http.StatusBadRequest, "validation_error", "no enabled root paths to scan")
		return
	}
	rootIDs := make([]string, 0, len(roots))
	for _, root := range roots {
		rootIDs = append(rootIDs, root.ID)
	}
	if req.Reason == "" {
		req.Reason = "manual"
	}
	payload, err := json.Marshal(scanner.GlobalScanPayload{
		SchemaVersion:  scanner.GlobalScanPayloadSchema,
		RootPathIDs:    rootIDs,
		Reason:         req.Reason,
		FollowSymlinks: false,
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "serialization_error", err.Error())
		return
	}
	ref, err := h.jobStore.Enqueue(r.Context(), jobs.EnqueueRequest{
		JobType:        "global_scan",
		Actor:          "api",
		CorrelationID:  CorrelationIDFromContext(r.Context()),
		IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
		LockKey:        "global_scan",
		Payload:        payload,
	})
	if err != nil {
		if errors.Is(err, jobs.ErrIdempotencyConflict) {
			writeError(w, r, http.StatusConflict, "idempotency_conflict", err.Error())
			return
		}
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, ref)
}

func (h *ScannerHandler) scanRoots(r *http.Request, ids []string) ([]scanner.RootPath, error) {
	if len(ids) > 0 {
		return h.store.RootPathsByIDs(r.Context(), ids)
	}
	return h.store.EnabledRootPaths(r.Context())
}

func (h *ScannerHandler) GetScan(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobStore.Get(r.Context(), chi.URLParam(r, "job_id"))
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "job not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if job.JobType != "global_scan" {
		writeError(w, r, http.StatusNotFound, "not_found", "global scan job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *ScannerHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListProjects(r.Context(), scanner.ProjectListOptions{
		ListOptions:  scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		RootPathID:   r.URL.Query().Get("root_path_id"),
		RepositoryID: r.URL.Query().Get("repository_id"),
		Status:       r.URL.Query().Get("status"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.GetProject(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListProjectLinks(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if _, err := h.store.GetProject(r.Context(), projectID); err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListProjectLinks(r.Context(), scanner.ProjectLinkListOptions{
		ListOptions: scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProjectID:   projectID,
		LinkType:    r.URL.Query().Get("link_type"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetProjectScanSettings(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.GetProjectSettings(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) PutProjectScanSettings(w http.ResponseWriter, r *http.Request) {
	var req scanner.ProjectScanSettingsInput
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := h.store.UpsertProjectSettings(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) CreateProjectScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"project_id"`
		ScanType  string `json:"scan_type"`
		RuleSetID string `json:"rule_set_id"`
		Reason    string `json:"reason"`
	}
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if req.ProjectID == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "project_id is required")
		return
	}
	project, err := h.store.GetProject(r.Context(), req.ProjectID)
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	if project.Status == scanner.ProjectStatusMissing {
		writeError(w, r, http.StatusBadRequest, "validation_error", "missing projects cannot be scanned until rediscovered")
		return
	}
	if project.Status == scanner.ProjectStatusDisabled {
		writeError(w, r, http.StatusBadRequest, "validation_error", "disabled projects cannot be scanned")
		return
	}
	if req.ScanType == "" {
		settings, err := h.store.GetProjectSettings(r.Context(), project.ID)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if !settings.ScanEnabled {
			writeError(w, r, http.StatusBadRequest, "validation_error", "project scan is disabled")
			return
		}
		req.ScanType = settings.ScanType
	}
	if !validHTTPProjectScanType(req.ScanType) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "unsupported scan_type")
		return
	}
	projectScanID := scannerNewOpaqueID("project_scan")
	jobID := jobs.NewJobID()
	groupID := "project_scan:" + projectScanID
	payload, err := json.Marshal(scanner.ProjectScanPayload{
		SchemaVersion: scanner.ProjectScanPayloadSchema,
		ProjectID:     project.ID,
		ProjectScanID: projectScanID,
		ScanType:      req.ScanType,
		RuleSetID:     req.RuleSetID,
		Reason:        req.Reason,
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "serialization_error", err.Error())
		return
	}
	ref, err := h.jobStore.Enqueue(r.Context(), jobs.EnqueueRequest{
		ID:             jobID,
		JobType:        "project_scan",
		Actor:          "api",
		CorrelationID:  CorrelationIDFromContext(r.Context()),
		IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
		JobGroupID:     groupID,
		WorkflowID:     projectScanID,
		LockKey:        "project_scan:" + project.ID,
		Payload:        payload,
	})
	if err != nil {
		if errors.Is(err, jobs.ErrIdempotencyConflict) {
			writeError(w, r, http.StatusConflict, "idempotency_conflict", err.Error())
			return
		}
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := h.store.CreateProjectScan(r.Context(), project, projectScanID, req.ScanType, req.RuleSetID, ref.JobID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, scanner.ProjectScanRef{
		ProjectScanID: item.ID,
		JobID:         ref.JobID,
		JobGroupID:    "project_scan:" + item.ID,
		Status:        ref.Status,
		SchemaVersion: jobs.JobRefSchemaVersion,
	})
}

func (h *ScannerHandler) ListProjectScans(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListProjectScans(r.Context(), scanner.ProjectScanListOptions{
		ListOptions: scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProjectID:   r.URL.Query().Get("project_id"),
		Status:      r.URL.Query().Get("status"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetProjectScan(w http.ResponseWriter, r *http.Request) {
	if err := h.jobStore.ReconcileWorkflowStatuses(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	item, err := h.store.GetProjectScan(r.Context(), chi.URLParam(r, "project_scan_id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	_ = h.store.ApplyWorkflowAggregateToProjectScan(r.Context(), "project_scan:"+item.ID)
	if refreshed, err := h.store.GetProjectScan(r.Context(), item.ID); err == nil {
		item = refreshed
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListProjectScanFindings(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListFindings(r.Context(), scanner.FindingListOptions{
		ListOptions:   scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProjectScanID: chi.URLParam(r, "project_scan_id"),
		Severity:      r.URL.Query().Get("severity"),
		Status:        r.URL.Query().Get("status"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var autoSync *bool
	if raw := r.URL.Query().Get("auto_sync_enabled"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "auto_sync_enabled must be boolean")
			return
		}
		autoSync = &parsed
	}
	page, err := h.store.ListRepositories(r.Context(), scanner.RepositoryListOptions{
		ListOptions:     scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		Provider:        r.URL.Query().Get("provider"),
		ProviderHost:    r.URL.Query().Get("provider_host"),
		FullPath:        r.URL.Query().Get("full_path"),
		Status:          r.URL.Query().Get("status"),
		DiscoverySource: r.URL.Query().Get("discovery_source"),
		AutoSyncEnabled: autoSync,
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetRepository(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.GetRepository(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListIgnoreRules(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListIgnoreRules(r.Context(), scanner.IgnoreRuleListOptions{
		ListOptions: scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ScopeType:   r.URL.Query().Get("scope_type"),
		ScopeID:     r.URL.Query().Get("scope_id"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) PutIgnoreRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IgnoreRules []scanner.IgnoreRuleInput `json:"ignore_rules"`
	}
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	items, err := h.store.UpsertIgnoreRules(r.Context(), req.IgnoreRules)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (h *ScannerHandler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListEnvironments(r.Context(), scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.GetEnvironment(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListWorkspaces(r.Context(), scanner.WorkspaceListOptions{
		ListOptions:   scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProjectID:     r.URL.Query().Get("project_id"),
		EnvironmentID: r.URL.Query().Get("environment_id"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.GetWorkspace(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListSecurityFindings(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListFindings(r.Context(), scanner.FindingListOptions{
		ListOptions:  scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProjectID:    r.URL.Query().Get("project_id"),
		RepositoryID: r.URL.Query().Get("repository_id"),
		Severity:     r.URL.Query().Get("severity"),
		Status:       r.URL.Query().Get("status"),
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) GetSecurityFinding(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.GetFinding(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListSecurityRuleSets(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var active *bool
	if raw := r.URL.Query().Get("active"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "active must be boolean")
			return
		}
		active = &parsed
	}
	page, err := h.store.ListRuleSets(r.Context(), scanner.RuleSetListOptions{ListOptions: scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")}, Active: active})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func (h *ScannerHandler) PutSecurityRuleSet(w http.ResponseWriter, r *http.Request) {
	var req scanner.SecurityRuleSetInput
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := h.store.UpsertRuleSet(r.Context(), req)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ScannerHandler) ListToolProfiles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var active *bool
	if raw := r.URL.Query().Get("active"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "active must be boolean")
			return
		}
		active = &parsed
	}
	page, err := h.store.ListToolProfiles(r.Context(), scanner.ToolProfileListOptions{
		ListOptions: scanner.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		Tool:        r.URL.Query().Get("tool"),
		SourceType:  r.URL.Query().Get("source_type"),
		Active:      active,
	})
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
}

func writeScannerReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, scanner.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "record not found")
	case errors.Is(err, scanner.ErrInvalidCursor):
		writeError(w, r, http.StatusBadRequest, "validation_error", "invalid cursor")
	case errors.Is(err, scanner.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
	}
}

func listResponse(items any, nextCursor string) map[string]any {
	return map[string]any{"items": items, "next_cursor": nullString(nextCursor)}
}

func scannerNewOpaqueID(prefix string) string {
	return prefix + "_" + jobs.NewJobID()
}

func validHTTPProjectScanType(value string) bool {
	switch value {
	case scanner.ScanTypeTerraformStatic, scanner.ScanTypeTerraformValidate, scanner.ScanTypeTerraformSecurity, scanner.ScanTypeTerraformFull, scanner.ScanTypeSecurityValidation:
		return true
	default:
		return false
	}
}
