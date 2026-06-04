package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/httpapi"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/artBass-rip/t-helper/internal/scanner"
)

func TestStage04ScannerRegistryEndpoints(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	configStore := config.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	if err := moduleStore.Seed(ctx); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewConfigHandler(configStore),
		httpapi.NewModulesHandler(configStore, moduleStore),
		httpapi.NewJobsHandler(jobStore),
		httpapi.NewStatusHandler(jobStore),
		httpapi.NewScannerHandler(scannerStore, jobStore),
	)

	rootPath := filepath.Join(t.TempDir(), "scan-root")
	body, _ := json.Marshal(map[string]any{
		"root_paths": []map[string]any{{
			"name":             "local",
			"path":             rootPath,
			"enabled":          true,
			"schedule_enabled": false,
		}},
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/root-paths", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/root-paths status = %d body = %s", rec.Code, rec.Body.String())
	}
	var roots struct {
		Items []scanner.RootPath `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&roots); err != nil {
		t.Fatalf("decode root paths: %v", err)
	}
	if len(roots.Items) != 1 || roots.Items[0].Path != rootPath {
		t.Fatalf("unexpected root paths response: %+v", roots)
	}
	if roots.Items[0].Source != scanner.RootPathSourceAPI {
		t.Fatalf("API-created root path source = %q, want api", roots.Items[0].Source)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/root-paths", bytes.NewReader([]byte(`{"root_paths":[{"name":"bad","path":"`+filepath.Join(t.TempDir(), "bad-root")+`","source":"config"}]}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/root-paths with source status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ignore-rules", bytes.NewReader([]byte(`{"ignore_rules":[{"scope_type":"root_path","scope_id":"`+roots.Items[0].ID+`","pattern":"ignored/","origin":"ui"},{"scope_type":"root_path","scope_id":"`+roots.Items[0].ID+`","pattern":"!ignored/keep","origin":"ui"}]}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/ignore-rules status = %d body = %s", rec.Code, rec.Body.String())
	}
	var ignoreRules struct {
		Items []scanner.IgnoreRule `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&ignoreRules); err != nil {
		t.Fatalf("decode ignore rules: %v", err)
	}
	if len(ignoreRules.Items) != 2 || ignoreRules.Items[1].Pattern != "!ignored/keep" {
		t.Fatalf("negative ignore rule was not preserved: %+v", ignoreRules.Items)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/root-paths", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/root-paths status = %d body = %s", rec.Code, rec.Body.String())
	}

	now := time.Now().UTC()
	project, _, err := scannerStore.UpsertProject(ctx, roots.Items[0], "service", now)
	if err != nil {
		t.Fatalf("upsert project fixture: %v", err)
	}
	timeText := now.Format(time.RFC3339Nano)
	if _, err := handle.DB.ExecContext(ctx, `INSERT INTO environments (id, name, code, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "env_http_stage04", "Production", "prod", timeText, timeText); err != nil {
		t.Fatalf("insert environment fixture: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `INSERT INTO workspaces (id, project_id, environment_id, name, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "workspace_http_stage04", project.ID, "env_http_stage04", "prod", 1, timeText, timeText); err != nil {
		t.Fatalf("insert workspace fixture: %v", err)
	}

	for _, path := range []string{"/api/environments/env_http_stage04", "/api/workspaces/workspace_http_stage04"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body = %s", path, rec.Code, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces?project_id="+project.ID+"&environment_id=env_http_stage04", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/workspaces filters status = %d body = %s", rec.Code, rec.Body.String())
	}
	var filteredWorkspaces struct {
		Items []scanner.Workspace `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&filteredWorkspaces); err != nil {
		t.Fatalf("decode filtered workspaces: %v", err)
	}
	if len(filteredWorkspaces.Items) != 1 || filteredWorkspaces.Items[0].ID != "workspace_http_stage04" {
		t.Fatalf("unexpected filtered workspaces: %+v", filteredWorkspaces.Items)
	}

	rec = httptest.NewRecorder()
	scanReq := httptest.NewRequest(http.MethodPost, "/api/scans", bytes.NewReader([]byte(`{"root_path_ids":["`+roots.Items[0].ID+`"],"reason":"manual"}`)))
	scanReq.Header.Set("Idempotency-Key", "stage04-scan")
	handler.ServeHTTP(rec, scanReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /api/scans status = %d body = %s", rec.Code, rec.Body.String())
	}
	var ref jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&ref); err != nil {
		t.Fatalf("decode scan ref: %v", err)
	}
	if ref.SchemaVersion != jobs.JobRefSchemaVersion || ref.Status != jobs.StatusQueued {
		t.Fatalf("unexpected scan ref: %+v", ref)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/scans/"+ref.JobID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/scans/{id} status = %d body = %s", rec.Code, rec.Body.String())
	}
	var scanJob jobs.Job
	if err := json.NewDecoder(rec.Body).Decode(&scanJob); err != nil {
		t.Fatalf("decode scan job: %v", err)
	}
	if scanJob.JobType != "global_scan" {
		t.Fatalf("GET /api/scans returned non-scan job: %+v", scanJob)
	}

	for _, path := range []string{"/api/projects", "/api/environments", "/api/workspaces"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body = %s", path, rec.Code, rec.Body.String())
		}
	}

	otherProject, _, err := scannerStore.UpsertProject(ctx, roots.Items[0], "other-service", now)
	if err != nil {
		t.Fatalf("upsert linked project fixture: %v", err)
	}
	repo, _, _, err := scannerStore.UpsertGenericRepository(ctx, roots.Items[0], rootPath)
	if err != nil {
		t.Fatalf("upsert repository fixture: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/repos?provider=generic&provider_host=local&full_path=.&discovery_source=filesystem&auto_sync_enabled=false", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos filters status = %d body = %s", rec.Code, rec.Body.String())
	}
	var repos struct {
		Items []scanner.Repository `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&repos); err != nil {
		t.Fatalf("decode filtered repos: %v", err)
	}
	if len(repos.Items) != 1 || repos.Items[0].ID != repo.ID {
		t.Fatalf("unexpected filtered repos: %+v", repos.Items)
	}
	if _, err := scannerStore.UpsertProjectLink(ctx, project.ID, otherProject.ID, repo.ID, ""); err != nil {
		t.Fatalf("upsert project link fixture: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/links", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/projects/{id}/links status = %d body = %s", rec.Code, rec.Body.String())
	}
	var links struct {
		Items []scanner.ProjectLink `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&links); err != nil {
		t.Fatalf("decode project links: %v", err)
	}
	if len(links.Items) != 1 || links.Items[0].LinkType != scanner.LinkTypeSameRepository || links.Items[0].RepositoryID != repo.ID {
		t.Fatalf("unexpected project links response: %+v", links.Items)
	}

	missing, _, err := scannerStore.UpsertProject(ctx, roots.Items[0], "missing-service", now)
	if err != nil {
		t.Fatalf("upsert missing project fixture: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `UPDATE projects SET status = ? WHERE id = ?`, scanner.ProjectStatusMissing, missing.ID); err != nil {
		t.Fatalf("mark project missing: %v", err)
	}
	disabled, _, err := scannerStore.UpsertProject(ctx, roots.Items[0], "disabled-service", now)
	if err != nil {
		t.Fatalf("upsert disabled project fixture: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `UPDATE projects SET status = ? WHERE id = ?`, scanner.ProjectStatusDisabled, disabled.ID); err != nil {
		t.Fatalf("mark project disabled: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects?status=disabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/projects?status=disabled status = %d body = %s", rec.Code, rec.Body.String())
	}
	var disabledProjects struct {
		Items []scanner.Project `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&disabledProjects); err != nil {
		t.Fatalf("decode disabled projects: %v", err)
	}
	if len(disabledProjects.Items) != 1 || disabledProjects.Items[0].ID != disabled.ID {
		t.Fatalf("unexpected disabled projects: %+v", disabledProjects.Items)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects?status=all", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/projects?status=all status = %d body = %s", rec.Code, rec.Body.String())
	}
	var allProjects struct {
		Items []scanner.Project `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&allProjects); err != nil {
		t.Fatalf("decode all projects: %v", err)
	}
	if len(allProjects.Items) < 4 {
		t.Fatalf("expected all project statuses to be returned, got %+v", allProjects.Items)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/project-scans", bytes.NewReader([]byte(`{"project_id":"`+missing.ID+`"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing project scan guard status = %d body = %s", rec.Code, rec.Body.String())
	}
	var jobsBeforeInvalidRuleSet int
	if err := handle.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&jobsBeforeInvalidRuleSet); err != nil {
		t.Fatalf("count jobs before invalid rule set: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/project-scans", bytes.NewReader([]byte(`{"project_id":"`+project.ID+`","rule_set_id":"missing_rule_set"}`))))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invalid rule set project scan status = %d body = %s", rec.Code, rec.Body.String())
	}
	var jobsAfterInvalidRuleSet int
	if err := handle.DB.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&jobsAfterInvalidRuleSet); err != nil {
		t.Fatalf("count jobs after invalid rule set: %v", err)
	}
	if jobsAfterInvalidRuleSet != jobsBeforeInvalidRuleSet {
		t.Fatalf("invalid rule set should not enqueue a job: before=%d after=%d", jobsBeforeInvalidRuleSet, jobsAfterInvalidRuleSet)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/project-scans", bytes.NewReader([]byte(`{"project_id":"`+project.ID+`","scan_type":"terraform_validate"}`))))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("project scan default rule set status = %d body = %s", rec.Code, rec.Body.String())
	}
	var projectScanRef scanner.ProjectScanRef
	if err := json.NewDecoder(rec.Body).Decode(&projectScanRef); err != nil {
		t.Fatalf("decode project scan ref: %v", err)
	}
	projectScanJob, err := jobStore.Get(ctx, projectScanRef.JobID)
	if err != nil {
		t.Fatalf("get project scan job: %v", err)
	}
	var scanPayload scanner.ProjectScanPayload
	if err := json.Unmarshal(projectScanJob.Payload, &scanPayload); err != nil {
		t.Fatalf("decode project scan payload: %v", err)
	}
	if scanPayload.RuleSetID != scanner.DefaultSecurityRuleSetID {
		t.Fatalf("project scan payload rule_set_id = %q, want default %q", scanPayload.RuleSetID, scanner.DefaultSecurityRuleSetID)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid projects cursor status = %d body = %s", rec.Code, rec.Body.String())
	}
}
