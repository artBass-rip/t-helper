package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	if err := jobStore.AddEvent(ctx, jobs.Event{
		JobID:      ref.JobID,
		JobGroupID: "config_operation:" + ref.JobID,
		EventType:  jobs.EventProgress,
		Status:     jobs.StatusQueued,
		Payload:    json.RawMessage(`{"schema_version":"job_events.payload.v1","message":"queued for test","details":{"phase":"acceptance"}}`),
	}); err != nil {
		t.Fatalf("add progress event: %v", err)
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
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/jobs?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/jobs invalid cursor status = %d body = %s", rec.Code, rec.Body.String())
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
	for _, key := range []string{jobs.StatusQueued, jobs.StatusRunning, jobs.StatusSucceeded, jobs.StatusFailed, jobs.StatusCancelled} {
		if _, ok := runtimeStatus.Jobs[key]; !ok {
			t.Fatalf("runtime status missing jobs key %q: %+v", key, runtimeStatus.Jobs)
		}
	}
	for _, key := range []string{"running", "stopped", "failed", "unavailable"} {
		if _, ok := runtimeStatus.Modules[key]; !ok {
			t.Fatalf("runtime status missing modules key %q: %+v", key, runtimeStatus.Modules)
		}
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
	latest, ok := jobStatus.LatestEvent.(map[string]any)
	if !ok || latest["message"] != "queued for test" {
		t.Fatalf("latest event did not include payload message: %#v", jobStatus.LatestEvent)
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
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/workflows?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/status/workflows invalid cursor status = %d body = %s", rec.Code, rec.Body.String())
	}

	if _, err := handle.DB.ExecContext(ctx, "DELETE FROM workflow_statuses"); err != nil {
		t.Fatalf("delete workflow statuses: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/workflows", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status/workflows self-heal status = %d body = %s", rec.Code, rec.Body.String())
	}
	var healedWorkflows struct {
		Items []jobs.WorkflowStatus `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&healedWorkflows); err != nil {
		t.Fatalf("decode healed workflows: %v", err)
	}
	if len(healedWorkflows.Items) != 1 || healedWorkflows.Items[0].JobGroupID != "config_operation:"+ref.JobID {
		t.Fatalf("workflow list did not reconcile missing read model: %+v", healedWorkflows)
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

	if _, ok, err := jobStore.ClaimNext(ctx, jobs.ClaimOptions{WorkerID: "host:1:worker", LeaseDuration: time.Minute}); err != nil || !ok {
		t.Fatalf("claim worker status job: ok=%v err=%v", ok, err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/workers?limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status/workers?limit=1 status = %d body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&workers); err != nil {
		t.Fatalf("decode paged workers: %v", err)
	}
	if len(workers.Items) != 1 || workers.Items[0].WorkerID != "host:1:worker" {
		t.Fatalf("unexpected paged workers: %+v", workers)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status/workers?cursor=not-a-cursor", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/status/workers invalid cursor status = %d body = %s", rec.Code, rec.Body.String())
	}
}
