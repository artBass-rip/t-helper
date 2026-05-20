package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/go-chi/chi/v5"
)

type JobsHandler struct {
	store *jobs.Store
}

func NewJobsHandler(store *jobs.Store) *JobsHandler {
	return &JobsHandler{store: store}
}

func (h *JobsHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.ListPage(r.Context(), jobs.ListFilters{
		JobType:     r.URL.Query().Get("job_type"),
		Status:      r.URL.Query().Get("status"),
		LockKey:     r.URL.Query().Get("lock_key"),
		JobGroupID:  r.URL.Query().Get("job_group_id"),
		ParentJobID: r.URL.Query().Get("parent_job_id"),
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
	})
	if err != nil {
		if errors.Is(err, jobs.ErrInvalidCursor) {
			writeError(w, r, http.StatusBadRequest, "validation_error", "invalid cursor")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": nullString(page.NextCursor)})
}

func (h *JobsHandler) Get(w http.ResponseWriter, r *http.Request) {
	job, err := h.store.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "job not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

type StatusHandler struct {
	store *jobs.Store
}

func NewStatusHandler(store *jobs.Store) *StatusHandler {
	return &StatusHandler{store: store}
}

func (h *StatusHandler) Runtime(w http.ResponseWriter, r *http.Request) {
	status, err := h.store.RuntimeStatus(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *StatusHandler) Workflows(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if err := h.store.ReconcileWorkflowStatuses(r.Context()); err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	page, err := h.store.WorkflowStatusesPage(r.Context(), r.URL.Query().Get("workflow_type"), r.URL.Query().Get("aggregate_status"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		if errors.Is(err, jobs.ErrInvalidCursor) {
			writeError(w, r, http.StatusBadRequest, "validation_error", "invalid cursor")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": nullString(page.NextCursor)})
}

func (h *StatusHandler) Workflow(w http.ResponseWriter, r *http.Request) {
	item, err := h.store.WorkflowStatus(r.Context(), chi.URLParam(r, "job_group_id"))
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "workflow status not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *StatusHandler) Job(w http.ResponseWriter, r *http.Request) {
	status, err := h.store.JobStatus(r.Context(), chi.URLParam(r, "job_id"))
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "job not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *StatusHandler) Workers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.store.WorkerStatusesPage(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		if errors.Is(err, jobs.ErrInvalidCursor) {
			writeError(w, r, http.StatusBadRequest, "validation_error", "invalid cursor")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": nullString(page.NextCursor)})
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
