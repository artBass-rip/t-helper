package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

func TestStage05CloneConflictDoesNotCreateNewRootPath(t *testing.T) {
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

	newRootPath := filepath.Join(t.TempDir(), "new-root")
	conflictBody, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/repo.git",
		"new_root_path":    newRootPath,
		"target_directory": "repo",
	})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(conflictBody)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting clone status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, err := scannerStore.RootPathByPath(ctx, newRootPath); !errors.Is(err, scanner.ErrNotFound) {
		t.Fatalf("new root path side effect error = %v, want not found", err)
	}
}

func TestStage05CloneWithNewRootPathCreatesRootPath(t *testing.T) {
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
	newRootPath := filepath.Join(t.TempDir(), "new-root")
	body, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/repo.git",
		"new_root_path":    newRootPath,
		"target_directory": "repo",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("clone with new root status = %d body = %s", rec.Code, rec.Body.String())
	}
	normalizedRootPath, err := repositorydomain.NormalizeRootPath(newRootPath)
	if err != nil {
		t.Fatalf("normalize new root path: %v", err)
	}
	root, err := scannerStore.RootPathByPath(ctx, normalizedRootPath)
	if err != nil {
		t.Fatalf("new root path was not created: %v", err)
	}
	if !root.Enabled || root.Source != scanner.RootPathSourceAPI {
		t.Fatalf("unexpected new root path: %+v", root)
	}
	var ref jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&ref); err != nil {
		t.Fatalf("decode job ref: %v", err)
	}
	job, err := jobStore.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get clone job: %v", err)
	}
	var payload repositorydomain.RepoClonePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode clone payload: %v", err)
	}
	if payload.RootPathID != root.ID || payload.TargetDirectory != "repo" {
		t.Fatalf("unexpected clone payload root/target: %+v", payload)
	}
}

func TestStage05CloneNewRootPathReleasesTemporaryReservationAfterWorker(t *testing.T) {
	requireHTTPRepositoryGit(t)
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
	source := createHTTPGitFixtureRepository(t)
	newRootPath := filepath.Join(t.TempDir(), "new-root")
	body, _ := json.Marshal(map[string]any{
		"provider":         "generic",
		"protocol":         "https",
		"clone_url":        source,
		"new_root_path":    newRootPath,
		"target_directory": "repo",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("clone with new root status = %d body = %s", rec.Code, rec.Body.String())
	}
	var ref jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&ref); err != nil {
		t.Fatalf("decode job ref: %v", err)
	}
	job, err := jobStore.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get clone job: %v", err)
	}
	repoHandlers := repositorydomain.JobHandlers(repoStore, scannerStore)
	if _, err := repoHandlers["repo_clone"].Handle(ctx, jobs.HandlerEnv{}, job); err != nil {
		t.Fatalf("handle clone job: %v", err)
	}
	var held int
	if err := handle.DB.QueryRowContext(ctx, `SELECT count(*) FROM repository_operation_reservations WHERE status = 'held'`).Scan(&held); err != nil {
		t.Fatalf("count held operation reservations: %v", err)
	}
	if held != 0 {
		t.Fatalf("held operation reservations after clone = %d, want 0", held)
	}
}

func TestStage05CredentialsAPIMasksSecretRefs(t *testing.T) {
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
	instances, err := repoStore.UpsertProviderInstances(ctx, []repositorydomain.ProviderInstanceInput{{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"repository_credentials": []map[string]any{{
			"provider_instance_id": instances[0].ID,
			"name":                 "git-token",
			"auth_type":            repositorydomain.AuthTypeHTTPSToken,
			"secret_ref":           "secretref://env/GITHUB_TOKEN",
			"usages":               []string{repositorydomain.UsageGitTransport},
		}},
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/repo-credentials", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put credentials status = %d body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Items []repositorydomain.Credential `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode put credentials: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].SecretRef != "secretref://env/***" {
		t.Fatalf("put credential secret_ref was not masked: %+v", response.Items)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/repo-credentials?provider_instance_id="+instances[0].ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list credentials status = %d body = %s", rec.Code, rec.Body.String())
	}
	response.Items = nil
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode list credentials: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].SecretRef != "secretref://env/***" {
		t.Fatalf("listed credential secret_ref was not masked: %+v", response.Items)
	}
}

func TestStage05CloneRejectsCredentialProtocolMismatch(t *testing.T) {
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
	enabled := true
	instances, err := repoStore.UpsertProviderInstances(ctx, []repositorydomain.ProviderInstanceInput{{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
		Enabled:      &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	credentials, err := repoStore.UpsertCredentials(ctx, []repositorydomain.CredentialInput{{
		ProviderInstanceID: instances[0].ID,
		Name:               "ssh",
		AuthType:           repositorydomain.AuthTypeSSHKey,
		SecretRef:          "secretref://env/GITHUB_SSH_KEY",
		Usages:             []string{repositorydomain.UsageGitTransport},
		Enabled:            &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_instance_id": instances[0].ID,
		"credential_id":        credentials[0].ID,
		"provider":             "github",
		"protocol":             "https",
		"clone_url":            "https://github.com/example/repo.git",
		"root_path_id":         root.ID,
		"target_directory":     "repo",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("protocol mismatch status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode protocol mismatch error: %v", err)
	}
	if apiErr.Error.Code != "credential_auth_type_protocol_mismatch" {
		t.Fatalf("protocol mismatch code = %q", apiErr.Error.Code)
	}
}

func TestStage05CloneRejectsUnsupportedCloneScope(t *testing.T) {
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
	body, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "https",
		"clone_url":        "https://github.com/example/repo.git",
		"root_path_id":     root.ID,
		"target_directory": "repo",
		"clone_scope":      "gitlab_group_recursive",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported clone_scope status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode unsupported clone_scope error: %v", err)
	}
	if apiErr.Error.Code != "unsupported_clone_scope" {
		t.Fatalf("unsupported clone_scope code = %q", apiErr.Error.Code)
	}
}

func TestStage05CloneRejectsAmbiguousTargetDirectoryFields(t *testing.T) {
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
	body, _ := json.Marshal(map[string]any{
		"provider":             "github",
		"protocol":             "https",
		"clone_url":            "https://github.com/example/repo.git",
		"root_path_id":         root.ID,
		"target_directory":     "repo",
		"new_target_directory": "other",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous target status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode ambiguous target error: %v", err)
	}
	if apiErr.Error.Code != "invalid_repository_path" {
		t.Fatalf("ambiguous target code = %q", apiErr.Error.Code)
	}
}

func TestStage05ProviderProfileAPIValidatesSafeHTTPSURLs(t *testing.T) {
	handle := openMigratedSQLite(t)
	defer handle.Close()

	jobStore := jobs.NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	repoStore := repositorydomain.NewStore(handle)
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewRepositoryHandler(repoStore, scannerStore, jobStore),
	)
	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "non_https_api_url",
			body: map[string]any{
				"repository_provider_instances": []map[string]any{{
					"provider":      "github",
					"provider_host": "github.com",
					"api_base_url":  "http://github.com/api",
				}},
			},
			code: "invalid_provider_profile_url",
		},
		{
			name: "userinfo_web_url",
			body: map[string]any{
				"repository_provider_instances": []map[string]any{{
					"provider":      "github",
					"provider_host": "github.com",
					"web_base_url":  "https://user@github.com/",
				}},
			},
			code: "credential_userinfo_not_allowed",
		},
		{
			name: "host_mismatch",
			body: map[string]any{
				"repository_provider_instances": []map[string]any{{
					"provider":      "github",
					"provider_host": "github.com",
					"api_base_url":  "https://example.com/api",
				}},
			},
			code: "invalid_provider_profile_url",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/repo-provider-instances", bytes.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("provider profile status = %d body = %s", rec.Code, rec.Body.String())
			}
			var apiErr struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
				t.Fatalf("decode provider profile error: %v", err)
			}
			if apiErr.Error.Code != tc.code {
				t.Fatalf("provider profile code = %q, want %q", apiErr.Error.Code, tc.code)
			}
		})
	}
}

func TestStage05RepositoryOperationPayloadsDoNotCarrySecretRefs(t *testing.T) {
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
	enabled := true
	instances, err := repoStore.UpsertProviderInstances(ctx, []repositorydomain.ProviderInstanceInput{{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
		Enabled:      &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	credentials, err := repoStore.UpsertCredentials(ctx, []repositorydomain.CredentialInput{{
		ProviderInstanceID: instances[0].ID,
		Name:               "token",
		AuthType:           repositorydomain.AuthTypeHTTPSToken,
		SecretRef:          "secretref://env/GITHUB_TOKEN",
		Usages:             []string{repositorydomain.UsageGitTransport},
		Enabled:            &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert credential: %v", err)
	}

	cloneBody, _ := json.Marshal(map[string]any{
		"provider_instance_id": instances[0].ID,
		"credential_id":        credentials[0].ID,
		"provider":             "github",
		"protocol":             "https",
		"clone_url":            "https://github.com/example/clone-payload.git",
		"root_path_id":         root.ID,
		"target_directory":     "clone-payload",
	})
	cloneRef := postRepositoryOperation(t, handler, "/api/repos/clone", cloneBody)
	assertJobPayloadCredentialOnly(t, ctx, jobStore, cloneRef.JobID, credentials[0].ID)

	pullRepo, err := repoStore.UpsertRepository(ctx, repositorydomain.Identity{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/pull-payload",
		CloneURL:     "https://github.com/example/pull-payload.git",
		Protocol:     repositorydomain.ProtocolHTTPS,
	}, root, "pull-payload", filepath.Join(root.Path, "pull-payload"), instances[0].ID, credentials[0].ID)
	if err != nil {
		t.Fatalf("upsert pull repository: %v", err)
	}
	pullBody, _ := json.Marshal(map[string]any{"repository_id": pullRepo.ID, "credential_id": credentials[0].ID})
	pullRef := postRepositoryOperation(t, handler, "/api/repos/pull", pullBody)
	assertJobPayloadCredentialOnly(t, ctx, jobStore, pullRef.JobID, credentials[0].ID)

	syncRepo, err := repoStore.UpsertRepository(ctx, repositorydomain.Identity{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/sync-payload",
		CloneURL:     "https://github.com/example/sync-payload.git",
		Protocol:     repositorydomain.ProtocolHTTPS,
	}, root, "sync-payload", filepath.Join(root.Path, "sync-payload"), instances[0].ID, credentials[0].ID)
	if err != nil {
		t.Fatalf("upsert sync repository: %v", err)
	}
	syncBody, _ := json.Marshal(map[string]any{"repository_id": syncRepo.ID, "credential_id": credentials[0].ID, "reason": "test"})
	syncRef := postRepositoryOperation(t, handler, "/api/repos/sync", syncBody)
	assertJobPayloadCredentialOnly(t, ctx, jobStore, syncRef.JobID, credentials[0].ID)
}

func TestStage05CloneConflictWithActivePullDoesNotMutateRepository(t *testing.T) {
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
	repo, err := repoStore.UpsertRepository(ctx, repositorydomain.Identity{
		Provider:     repositorydomain.ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/repo",
		CloneURL:     "https://github.com/example/repo.git",
		Protocol:     repositorydomain.ProtocolHTTPS,
	}, root, "repo", filepath.Join(root.Path, "repo"), "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	pullPayload, _ := json.Marshal(repositorydomain.RepoPullPayload{SchemaVersion: repositorydomain.RepoPullPayloadSchema, RepositoryID: repo.ID})
	pullRef, err := jobStore.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "repo_pull",
		Actor:   "api",
		LockKey: "repository:" + repo.ID,
		Payload: pullPayload,
	})
	if err != nil {
		t.Fatalf("enqueue pull: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider":         "github",
		"protocol":         "ssh",
		"clone_url":        "git@github.com:example/repo.git",
		"root_path_id":     root.ID,
		"target_directory": "changed-target",
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/clone", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("clone during pull status = %d body = %s", rec.Code, rec.Body.String())
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
	if conflict.Error.Code != "repository_operation_already_running" || conflict.Error.Details["active_job_id"] != pullRef.JobID || conflict.Error.Details["active_job_type"] != "repo_pull" {
		t.Fatalf("unexpected conflict details: %+v", conflict.Error)
	}
	reloaded, err := scannerStore.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("reload repository: %v", err)
	}
	if reloaded.TargetDirectory != "repo" || reloaded.LocalPath != filepath.Join(root.Path, "repo") || reloaded.AuthType != repositorydomain.ProtocolHTTPS {
		t.Fatalf("repository mutated before conflict: %+v", reloaded)
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

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/repos/sync", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sync superseded status = %d body = %s", rec.Code, rec.Body.String())
	}
	apiErr.Error.Code = ""
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode sync error: %v", err)
	}
	if apiErr.Error.Code != "repository_status_not_operable" {
		t.Fatalf("sync error code = %q", apiErr.Error.Code)
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

func postRepositoryOperation(t *testing.T, handler http.Handler, path string, body []byte) jobs.JobRef {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%s status = %d body = %s", path, rec.Code, rec.Body.String())
	}
	var ref jobs.JobRef
	if err := json.NewDecoder(rec.Body).Decode(&ref); err != nil {
		t.Fatalf("decode %s job ref: %v", path, err)
	}
	return ref
}

func assertJobPayloadCredentialOnly(t *testing.T, ctx context.Context, store *jobs.Store, jobID, credentialID string) {
	t.Helper()
	job, err := store.Get(ctx, jobID)
	if err != nil {
		t.Fatalf("get job %s: %v", jobID, err)
	}
	if !bytes.Contains(job.Payload, []byte(`"credential_id":"`+credentialID+`"`)) {
		t.Fatalf("payload does not carry credential_id %q: %s", credentialID, string(job.Payload))
	}
	if bytes.Contains(job.Payload, []byte("secretref://")) || bytes.Contains(job.Payload, []byte("GITHUB_TOKEN")) || bytes.Contains(job.Payload, []byte("secret_ref")) {
		t.Fatalf("payload leaked secret reference: %s", string(job.Payload))
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

func requireHTTPRepositoryGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
}

func createHTTPGitFixtureRepository(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	runHTTPGitCommand(t, "", "init", source)
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runHTTPGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "add", "README.md")
	runHTTPGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	return source
}

func runHTTPGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
