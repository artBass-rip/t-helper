package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/repository"
	"github.com/artBass-rip/t-helper/internal/scanner"
)

type RepositoryHandler struct {
	store        *repository.Store
	scannerStore *scanner.Store
	jobStore     *jobs.Store
}

func NewRepositoryHandler(store *repository.Store, scannerStore *scanner.Store, jobStore *jobs.Store) *RepositoryHandler {
	return &RepositoryHandler{store: store, scannerStore: scannerStore, jobStore: jobStore}
}

func (h *RepositoryHandler) ListProviderInstances(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var enabled *bool
	if raw := r.URL.Query().Get("enabled"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "enabled must be boolean")
			return
		}
		enabled = &parsed
	}
	items, err := h.store.ListProviderInstances(r.Context(), repository.ProviderInstanceListOptions{
		ListOptions:  repository.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		Provider:     r.URL.Query().Get("provider"),
		ProviderHost: r.URL.Query().Get("provider_host"),
		Enabled:      enabled,
	})
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (h *RepositoryHandler) PutProviderInstances(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepositoryProviderInstances []repository.ProviderInstanceInput `json:"repository_provider_instances"`
	}
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	items, err := h.store.UpsertProviderInstances(r.Context(), req.RepositoryProviderInstances)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (h *RepositoryHandler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.store.ListCredentials(r.Context(), repository.CredentialListOptions{
		ListOptions:        repository.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProviderInstanceID: r.URL.Query().Get("provider_instance_id"),
		Usage:              r.URL.Query().Get("usage"),
		AuthType:           r.URL.Query().Get("auth_type"),
	})
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (h *RepositoryHandler) PutCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepositoryCredentials []repository.CredentialInput `json:"repository_credentials"`
	}
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	items, err := h.store.UpsertCredentials(r.Context(), req.RepositoryCredentials)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	for idx := range items {
		items[idx].SecretRef = "secretref://env/***"
	}
	writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (h *RepositoryHandler) Clone(w http.ResponseWriter, r *http.Request) {
	var req repository.CloneRequest
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if req.CloneScope == "" {
		req.CloneScope = "single_repository"
	}
	var instance *repository.ProviderInstance
	if req.ProviderInstanceID != "" {
		item, err := h.store.GetProviderInstance(r.Context(), req.ProviderInstanceID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		instance = &item
	}
	identity, err := repository.NormalizeIdentity(req, instance)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if instance != nil && req.CredentialID != "" {
		if err := h.store.ValidateCredential(r.Context(), req.CredentialID, instance.ID, repository.UsageGitTransport); err != nil {
			writeRepositoryError(w, r, err)
			return
		}
	}
	root, err := h.cloneRoot(r, req)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	target := req.TargetDirectory
	if target == "" {
		target = req.NewTargetDirectory
	}
	targetDirectory, localPath, err := repository.NormalizeTarget(root.Path, target)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if active, conflictCode, err := h.activeCloneConflict(r, identity, root.ID, targetDirectory); err == nil {
		writeErrorDetails(w, r, http.StatusConflict, conflictCode, "repository clone conflict", map[string]any{
			"lock_key":        active.LockKey,
			"active_job_id":   active.ID,
			"active_job_type": active.JobType,
		})
		return
	} else if !errors.Is(err, jobs.ErrNotFound) {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	repo, err := h.store.UpsertRepository(r.Context(), identity, root, targetDirectory, localPath, req.ProviderInstanceID, req.CredentialID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	lockKey := "repository:" + repo.ID
	payload, _ := json.Marshal(repository.RepoClonePayload{
		SchemaVersion:      repository.RepoClonePayloadSchema,
		RepositoryID:       repo.ID,
		ProviderInstanceID: req.ProviderInstanceID,
		Provider:           identity.Provider,
		ProviderHost:       identity.ProviderHost,
		CredentialID:       req.CredentialID,
		Protocol:           identity.Protocol,
		CloneURL:           identity.CloneURL,
		CloneScope:         "single_repository",
		FullPath:           identity.FullPath,
		RootPathID:         root.ID,
		TargetDirectory:    targetDirectory,
		LocalPath:          localPath,
	})
	if replay, replayErr := h.jobStore.IdempotentReplay(r.Context(), jobs.EnqueueRequest{JobType: "repo_clone", Actor: "api", IdempotencyKey: r.Header.Get(idempotencyKeyHeader), Payload: payload}); replayErr == nil {
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, jobs.ErrNotFound) {
		writeEnqueueError(w, r, replayErr)
		return
	}
	if active, err := h.jobStore.ActiveRepositoryOperation(r.Context(), lockKey); err == nil {
		writeRepositoryConflict(w, r, repo.ID, lockKey, active)
		return
	} else if !errors.Is(err, jobs.ErrNotFound) {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	ref, err := h.jobStore.Enqueue(r.Context(), jobs.EnqueueRequest{
		JobType:        "repo_clone",
		Actor:          "api",
		CorrelationID:  CorrelationIDFromContext(r.Context()),
		IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
		LockKey:        lockKey,
		Payload:        payload,
	})
	if err != nil {
		writeEnqueueError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, ref)
}

func (h *RepositoryHandler) activeCloneConflict(r *http.Request, identity repository.Identity, rootPathID, targetDirectory string) (jobs.Job, string, error) {
	for _, status := range []string{jobs.StatusQueued, jobs.StatusRunning} {
		active, err := h.jobStore.List(r.Context(), jobs.ListFilters{JobType: "repo_clone", Status: status})
		if err != nil {
			return jobs.Job{}, "", err
		}
		for _, job := range active {
			if key := r.Header.Get(idempotencyKeyHeader); key != "" && job.IdempotencyKey == key {
				continue
			}
			var payload repository.RepoClonePayload
			if err := json.Unmarshal(job.Payload, &payload); err != nil {
				continue
			}
			if payload.Provider == identity.Provider && payload.ProviderHost == identity.ProviderHost && payload.FullPath == identity.FullPath {
				return job, "repository_operation_already_running", nil
			}
			if payload.RootPathID == rootPathID && payload.TargetDirectory == targetDirectory {
				return job, "repository_target_path_busy", nil
			}
		}
	}
	return jobs.Job{}, "", jobs.ErrNotFound
}

func (h *RepositoryHandler) Pull(w http.ResponseWriter, r *http.Request) {
	var req repository.PullRequest
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	h.enqueueExistingRepoOperation(w, r, "repo_pull", req.RepositoryID, req.CredentialID, repository.RepoPullPayload{SchemaVersion: repository.RepoPullPayloadSchema, RepositoryID: req.RepositoryID, CredentialID: req.CredentialID})
}

func (h *RepositoryHandler) Sync(w http.ResponseWriter, r *http.Request) {
	var req repository.SyncRequest
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	h.enqueueExistingRepoOperation(w, r, "repo_sync", req.RepositoryID, req.CredentialID, repository.RepoSyncPayload{SchemaVersion: repository.RepoSyncPayloadSchema, RepositoryID: req.RepositoryID, CredentialID: req.CredentialID, Reason: req.Reason})
}

func (h *RepositoryHandler) enqueueExistingRepoOperation(w http.ResponseWriter, r *http.Request, jobType, repositoryID, credentialID string, payloadValue any) {
	repo, err := h.scannerStore.GetRepository(r.Context(), repositoryID)
	if err != nil {
		writeScannerReadError(w, r, err)
		return
	}
	if repo.Status == "superseded" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "superseded repositories cannot be operated")
		return
	}
	if credentialID != "" && repo.ProviderInstanceID != "" {
		if err := h.store.ValidateCredential(r.Context(), credentialID, repo.ProviderInstanceID, repository.UsageGitTransport); err != nil {
			writeRepositoryError(w, r, err)
			return
		}
	}
	lockKey := "repository:" + repo.ID
	payload, _ := json.Marshal(payloadValue)
	if replay, replayErr := h.jobStore.IdempotentReplay(r.Context(), jobs.EnqueueRequest{JobType: jobType, Actor: "api", IdempotencyKey: r.Header.Get(idempotencyKeyHeader), Payload: payload}); replayErr == nil {
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, jobs.ErrNotFound) {
		writeEnqueueError(w, r, replayErr)
		return
	}
	if active, err := h.jobStore.ActiveRepositoryOperation(r.Context(), lockKey); err == nil {
		writeRepositoryConflict(w, r, repo.ID, lockKey, active)
		return
	} else if !errors.Is(err, jobs.ErrNotFound) {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	ref, err := h.jobStore.Enqueue(r.Context(), jobs.EnqueueRequest{
		JobType:        jobType,
		Actor:          "api",
		CorrelationID:  CorrelationIDFromContext(r.Context()),
		IdempotencyKey: r.Header.Get(idempotencyKeyHeader),
		LockKey:        lockKey,
		Payload:        payload,
	})
	if err != nil {
		writeEnqueueError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, ref)
}

func (h *RepositoryHandler) cloneRoot(r *http.Request, req repository.CloneRequest) (scanner.RootPath, error) {
	if req.RootPathID != "" {
		return h.scannerStore.GetRootPath(r.Context(), req.RootPathID)
	}
	if req.NewRootPath == "" {
		return scanner.RootPath{}, repository.ErrValidation
	}
	enabled := true
	items, err := h.scannerStore.UpsertRootPaths(r.Context(), []scanner.RootPathInput{{Path: req.NewRootPath, Enabled: &enabled, Source: scanner.RootPathSourceAPI}})
	if err != nil {
		return scanner.RootPath{}, err
	}
	return items[0], nil
}

func writeRepositoryConflict(w http.ResponseWriter, r *http.Request, repositoryID, lockKey string, active jobs.Job) {
	writeErrorDetails(w, r, http.StatusConflict, "repository_operation_already_running", "repository operation already running", map[string]any{
		"repository_id":   repositoryID,
		"lock_key":        lockKey,
		"active_job_id":   active.ID,
		"active_job_type": active.JobType,
	})
}

func writeEnqueueError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, jobs.ErrIdempotencyConflict) {
		writeError(w, r, http.StatusConflict, "idempotency_conflict", err.Error())
		return
	}
	writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
}

func writeRepositoryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound), errors.Is(err, scanner.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "record not found")
	case errors.Is(err, repository.ErrValidation), errors.Is(err, scanner.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
	}
}
