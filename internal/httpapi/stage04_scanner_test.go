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

	missing, _, err := scannerStore.UpsertProject(ctx, roots.Items[0], "missing-service", now)
	if err != nil {
		t.Fatalf("upsert missing project fixture: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `UPDATE projects SET status = ? WHERE id = ?`, scanner.ProjectStatusMissing, missing.ID); err != nil {
		t.Fatalf("mark project missing: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/project-scans", bytes.NewReader([]byte(`{"project_id":"`+missing.ID+`"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing project scan guard status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid projects cursor status = %d body = %s", rec.Code, rec.Body.String())
	}
}
