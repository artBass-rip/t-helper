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

	"github.com/artBass-rip/t-helper/internal/httpapi"
	"github.com/artBass-rip/t-helper/internal/jobs"
	repositorydomain "github.com/artBass-rip/t-helper/internal/repository"
	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/artBass-rip/t-helper/internal/scanner"
)

func TestStage05RepositoryCloneValidationCodeAndIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	repoStore := repositorydomain.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewRepositoryHandler(repoStore, scannerStore, jobStore),
	)
	root := upsertHTTPRepositoryRoot(t, ctx, scannerStore)

	badBody, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://user:token@github.com/example/repo.git",
		"root_path_id":     root.ID,
		"target_directory": "repo",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(badBody)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("credential userinfo status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if apiErr.Error.Code != "credential_userinfo_not_allowed" {
		t.Fatalf("error code = %q", apiErr.Error.Code)
	}

	body, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/repo.git",
		"root_path_id":     root.ID,
		"target_directory": "repo",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "clone-replay")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	var first jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&first); err != nil {
		t.Fatalf("decode first job ref: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body))
	req.Header.Set("Idempotency-Key", "clone-replay")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("replay clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	var replay jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&replay); err != nil {
		t.Fatalf("decode replay job ref: %v", err)
	}
	if replay.JobID != first.JobID {
		t.Fatalf("replay job id = %q, want %q", replay.JobID, first.JobID)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Error.Code != "repository_operation_already_running" {
		t.Fatalf("conflict code = %q", conflict.Error.Code)
	}
	if conflict.Error.Details["repository_id"] == "" || conflict.Error.Details["active_job_id"] != first.JobID || conflict.Error.Details["active_job_type"] != "repo_clone" {
		t.Fatalf("conflict details missing active repository operation fields: %+v", conflict.Error.Details)
	}

	changedBody, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/other.git",
		"root_path_id":     root.ID,
		"target_directory": "other",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(changedBody))
	req.Header.Set("Idempotency-Key", "clone-replay")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("changed idempotency replay status = %d body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode idempotency conflict: %v", err)
	}
	if conflict.Error.Code != "idempotency_conflict" {
		t.Fatalf("idempotency conflict code = %q", conflict.Error.Code)
	}
}

func TestStage05CloneRejectsBusyTargetPathForDifferentRepository(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	repoStore := repositorydomain.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewRepositoryHandler(repoStore, scannerStore, jobStore),
	)
	root := upsertHTTPRepositoryRoot(t, ctx, scannerStore)
	firstBody, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/repo.git",
		"root_path_id":     root.ID,
		"target_directory": "repo",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(firstBody)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	secondBody, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/other.git",
		"root_path_id":     root.ID,
		"target_directory": "repo",
	})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(secondBody)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("busy target clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode busy target error: %v", err)
	}
	if apiErr.Error.Code != "repository_target_path_busy" {
		t.Fatalf("busy target code = %q", apiErr.Error.Code)
	}
}

func TestStage05CloneRejectsDisabledProviderInstance(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	repoStore := repositorydomain.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewRepositoryHandler(repoStore, scannerStore, jobStore),
	)
	root := upsertHTTPRepositoryRoot(t, ctx, scannerStore)
	disabled := false
	instances, err := repoStore.UpsertProviderInstances(ctx, []repositorydomain.ProviderInstanceInput{{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
		Enabled:      &disabled,
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_instance_id": instances[0].ID,
		"provider":             "github",
		"protocol":             "https",
		"clone_url":            "https://github.com/example/repo.git",
		"root_path_id":         root.ID,
		"target_directory":     "repo",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled provider clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if apiErr.Error.Code != "provider_instance_disabled" {
		t.Fatalf("error code = %q", apiErr.Error.Code)
	}
}

func TestStage05PullRejectsSupersededRepository(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	repoStore := repositorydomain.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewRepositoryHandler(repoStore, scannerStore, jobStore),
	)
	root := upsertHTTPRepositoryRoot(t, ctx, scannerStore)
	repo, _, _, err := scannerStore.UpsertGenericRepository(ctx, root, filepath.Join(root.Path, "repo"))
	if err != nil {
		t.Fatalf("upsert generic repo: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `UPDATE repositories SET status = ?, superseded_by_repository_id = ?, updated_at = ? WHERE id = ?`, "superseded", repo.ID, time.Now().UTC().Format(time.RFC3339Nano), repo.ID); err != nil {
		t.Fatalf("mark superseded: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"repository_id": repo.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/pull", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pull superseded status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if apiErr.Error.Code != "repository_status_not_operable" {
		t.Fatalf("error code = %q", apiErr.Error.Code)
	}
}

func TestStage05PullAndSyncConflictAcrossRepositoryOperationTypes(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	repoStore := repositorydomain.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewRepositoryHandler(repoStore, scannerStore, jobStore),
	)
	root := upsertHTTPRepositoryRoot(t, ctx, scannerStore)
	repo, _, _, err := scannerStore.UpsertGenericRepository(ctx, root, filepath.Join(root.Path, "repo"))
	if err != nil {
		t.Fatalf("upsert generic repo: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"repository_id": repo.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/pull", bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("pull status = %d body = %s", rec.Code, rec.Body.String())
	}
	var pullRef jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&pullRef); err != nil {
		t.Fatalf("decode pull job ref: %v", err)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/sync", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("sync conflict status = %d body = %s", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode sync conflict: %v", err)
	}
	if conflict.Error.Code != "repository_operation_already_running" || conflict.Error.Details["active_job_id"] != pullRef.JobID || conflict.Error.Details["active_job_type"] != "repo_pull" {
		t.Fatalf("unexpected sync conflict: %+v", conflict.Error)
	}
}

func upsertHTTPRepositoryRoot(t *testing.T, ctx context.Context, store *scanner.Store) scanner.RootPath {
	t.Helper()
	enabled := true
	items, err := store.UpsertRootPaths(ctx, []scanner.RootPathInput{{Path: filepath.Join(t.TempDir(), "root"), Enabled: &enabled, Source: scanner.RootPathSourceAPI}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	return items[0]
}
