package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/httpapi"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/runtime"
)

func TestStage03JobsAndStatusEndpoints(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	configStore := config.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	if err := moduleStore.Seed(ctx); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	jobStore := jobs.NewStore(handle)
	ref, err := jobStore.Enqueue(ctx, jobs.EnqueueRequest{
		JobType: "config_reload",
		Payload: json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewConfigHandler(configStore),
		httpapi.NewModulesHandler(configStore, moduleStore),
		httpapi.NewJobsHandler(jobStore),
		httpapi.NewStatusHandler(jobStore),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/jobs status = %d body = %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []jobs.Job `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != ref.JobID {
		t.Fatalf("unexpected jobs list: %+v", list)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status status = %d body = %s", rec.Code, rec.Body.String())
	}
	var runtimeStatus jobs.RuntimeStatus
	if err := json.NewDecoder(rec.Body).Decode(&runtimeStatus); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if runtimeStatus.SchemaVersion != "runtime_status.v1" || runtimeStatus.Jobs[jobs.StatusQueued] != 1 {
		t.Fatalf("unexpected runtime status: %+v", runtimeStatus)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/jobs/"+ref.JobID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status/jobs/{id} status = %d body = %s", rec.Code, rec.Body.String())
	}
	var jobStatus jobs.JobStatus
	if err := json.NewDecoder(rec.Body).Decode(&jobStatus); err != nil {
		t.Fatalf("decode job status: %v", err)
	}
	if jobStatus.SchemaVersion != "job_status.v1" || jobStatus.JobID != ref.JobID {
		t.Fatalf("unexpected job status: %+v", jobStatus)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/workflows/config_operation:"+ref.JobID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status/workflows/{job_group_id} status = %d body = %s", rec.Code, rec.Body.String())
	}
	var workflow jobs.WorkflowStatus
	if err := json.NewDecoder(rec.Body).Decode(&workflow); err != nil {
		t.Fatalf("decode workflow status: %v", err)
	}
	if workflow.JobGroupID != "config_operation:"+ref.JobID || workflow.AggregateStatus != jobs.StatusQueued || workflow.ProgressTotal != 1 {
		t.Fatalf("unexpected workflow status: %+v", workflow)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/workers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status/workers status = %d body = %s", rec.Code, rec.Body.String())
	}
	var workers struct {
		Items []jobs.WorkerStatus `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&workers); err != nil {
		t.Fatalf("decode workers: %v", err)
	}
	if len(workers.Items) != 0 {
		t.Fatalf("expected no idle workers in Stage 03, got %+v", workers)
	}
}
