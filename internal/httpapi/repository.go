package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/repository"
	"github.com/artBass-rip/t-helper/internal/scanner"
)

type RepositoryHandler struct {
	store        *repository.Store
	scannerStore *scanner.Store
	jobStore     *jobs.Store
}

type cloneRootSelection struct {
	root       scanner.RootPath
	existing   bool
	reserveKey string
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
	page, err := h.store.ListProviderInstancesPage(r.Context(), repository.ProviderInstanceListOptions{
		ListOptions:  repository.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		Provider:     r.URL.Query().Get("provider"),
		ProviderHost: r.URL.Query().Get("provider_host"),
		Enabled:      enabled,
	})
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
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
	page, err := h.store.ListCredentialsPage(r.Context(), repository.CredentialListOptions{
		ListOptions:        repository.ListOptions{Limit: limit, Cursor: r.URL.Query().Get("cursor")},
		ProviderInstanceID: r.URL.Query().Get("provider_instance_id"),
		Usage:              r.URL.Query().Get("usage"),
		AuthType:           r.URL.Query().Get("auth_type"),
	})
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse(page.Items, page.NextCursor))
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
	if req.CloneScope != "single_repository" {
		writeError(w, r, http.StatusBadRequest, "unsupported_clone_scope", "only single_repository clone_scope is supported")
		return
	}
	var instance *repository.ProviderInstance
	if req.ProviderInstanceID != "" {
		item, err := h.store.GetProviderInstance(r.Context(), req.ProviderInstanceID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		if !item.Enabled {
			writeError(w, r, http.StatusBadRequest, "provider_instance_disabled", "provider instance is disabled")
			return
		}
		instance = &item
	}
	if req.CredentialID != "" && instance == nil {
		cred, err := h.store.GetCredential(r.Context(), req.CredentialID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		item, err := h.store.GetProviderInstance(r.Context(), cred.ProviderInstanceID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		if !item.Enabled {
			writeError(w, r, http.StatusBadRequest, "provider_instance_disabled", "provider instance is disabled")
			return
		}
		req.ProviderInstanceID = item.ID
		instance = &item
	}
	identity, err := repository.NormalizeIdentity(req, instance)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if instance != nil && req.CredentialID != "" {
		if err := h.store.ValidateCredentialForProtocol(r.Context(), req.CredentialID, instance.ID, repository.UsageGitTransport, identity.Protocol); err != nil {
			writeRepositoryError(w, r, err)
			return
		}
	}
	rootSelection, err := h.cloneRootPreview(r, req)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	root := rootSelection.root
	target := req.TargetDirectory
	if target == "" {
		target = req.NewTargetDirectory
	}
	targetDirectory, localPath, err := repository.NormalizeTarget(root.Path, target)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	pathReservationKey, err := repository.TargetReservationKey(root.Path, rootSelection.reserveKey, targetDirectory)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if replay, replayErr := h.cloneIdempotentReplay(r, identity, root.ID, targetDirectory, localPath, req.ProviderInstanceID, req.CredentialID); replayErr == nil {
		writeJSON(w, http.StatusAccepted, replay)
		return
	} else if !errors.Is(replayErr, jobs.ErrNotFound) {
		writeEnqueueError(w, r, replayErr)
		return
	}
	identityReservationKey := repository.IdentityReservationKey(identity.Provider, identity.ProviderHost, identity.FullPath)
	jobID := jobs.NewJobID()
	reservationOwner := jobID
	heldReservations, err := h.store.ReserveOperationKeys(r.Context(), reservationOwner, 5*time.Minute, identityReservationKey, pathReservationKey)
	if err != nil {
		var conflict repository.ReservationConflictError
		if errors.As(err, &conflict) {
			code := "repository_operation_already_running"
			if conflict.Key == pathReservationKey {
				code = "repository_target_path_busy"
			}
			if active, activeCode, conflictRepositoryID, activeErr := h.activeCloneConflict(r, identity, rootSelection.reserveKey, root.Path, pathReservationKey); activeErr == nil {
				writeErrorDetails(w, r, http.StatusConflict, activeCode, "repository clone conflict", map[string]any{
					"repository_id":   conflictRepositoryID,
					"lock_key":        active.LockKey,
					"active_job_id":   active.ID,
					"active_job_type": active.JobType,
				})
				return
			}
			writeErrorDetails(w, r, http.StatusConflict, code, "repository clone conflict", map[string]any{
				"lock_key": conflict.Key,
			})
			return
		}
		writeRepositoryError(w, r, err)
		return
	}
	releaseReservations := true
	defer func() {
		if releaseReservations {
			_ = h.store.ReleaseOperationReservations(r.Context(), reservationOwner, heldReservations...)
		}
	}()
	if active, conflictCode, conflictRepositoryID, err := h.activeCloneConflict(r, identity, rootSelection.reserveKey, root.Path, pathReservationKey); err == nil {
		writeErrorDetails(w, r, http.StatusConflict, conflictCode, "repository clone conflict", map[string]any{
			"repository_id":   conflictRepositoryID,
			"lock_key":        active.LockKey,
			"active_job_id":   active.ID,
			"active_job_type": active.JobType,
		})
		return
	} else if !errors.Is(err, jobs.ErrNotFound) {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if !rootSelection.existing {
		root, err = h.createCloneRoot(r, root.Path)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		targetDirectory, localPath, err = repository.NormalizeTarget(root.Path, targetDirectory)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
	}
	if existing, err := h.store.ExistingRepositoryForClone(r.Context(), identity, root.ID, localPath); err == nil {
		lockKey := "repository:" + existing.ID
		if active, err := h.jobStore.ActiveRepositoryOperation(r.Context(), lockKey); err == nil {
			writeRepositoryConflict(w, r, existing.ID, lockKey, active)
			return
		} else if !errors.Is(err, jobs.ErrNotFound) {
			writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		writeRepositoryError(w, r, err)
		return
	}
	repo, repositoryCreated, err := h.store.UpsertRepositoryForClone(r.Context(), identity, root, targetDirectory, localPath, req.ProviderInstanceID, req.CredentialID)
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
		RepositoryCreated:  repositoryCreated,
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
		ID:             jobID,
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
	if ref.JobID == jobID {
		releaseReservations = false
	}
	writeJSON(w, http.StatusAccepted, ref)
}

func (h *RepositoryHandler) cloneIdempotentReplay(r *http.Request, identity repository.Identity, rootPathID, targetDirectory, localPath, providerInstanceID, credentialID string) (jobs.JobRef, error) {
	key := r.Header.Get(idempotencyKeyHeader)
	if key == "" {
		return jobs.JobRef{}, jobs.ErrNotFound
	}
	job, err := h.jobStore.JobByIdempotency(r.Context(), "api", "repo_clone", key)
	if err != nil {
		return jobs.JobRef{}, err
	}
	var payload repository.RepoClonePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return jobs.JobRef{}, err
	}
	same := payload.Provider == identity.Provider &&
		payload.ProviderHost == identity.ProviderHost &&
		payload.FullPath == identity.FullPath &&
		payload.Protocol == identity.Protocol &&
		payload.CloneURL == identity.CloneURL &&
		payload.RootPathID == rootPathID &&
		payload.TargetDirectory == targetDirectory &&
		payload.LocalPath == localPath &&
		payload.ProviderInstanceID == providerInstanceID &&
		payload.CredentialID == credentialID
	if !same {
		return jobs.JobRef{}, jobs.ErrIdempotencyConflict
	}
	return jobs.JobRef{JobID: job.ID, Status: job.Status, SchemaVersion: jobs.JobRefSchemaVersion}, nil
}

func (h *RepositoryHandler) activeCloneConflict(r *http.Request, identity repository.Identity, rootReserveKey, rootPath, pathReservationKey string) (jobs.Job, string, string, error) {
	for _, status := range []string{jobs.StatusQueued, jobs.StatusRunning} {
		active, err := h.jobStore.List(r.Context(), jobs.ListFilters{JobType: "repo_clone", Status: status})
		if err != nil {
			return jobs.Job{}, "", "", err
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
				return job, "repository_operation_already_running", payload.RepositoryID, nil
			}
			if payload.RootPathID != "" {
				payloadRoot, err := h.scannerStore.GetRootPath(r.Context(), payload.RootPathID)
				if err != nil {
					continue
				}
				payloadRootKey := payload.RootPathID
				if payloadRoot.Path == rootPath {
					payloadRootKey = rootReserveKey
				}
				payloadPathKey, err := repository.TargetReservationKey(payloadRoot.Path, payloadRootKey, payload.TargetDirectory)
				if err == nil && payloadPathKey == pathReservationKey {
					return job, "repository_target_path_busy", payload.RepositoryID, nil
				}
			}
		}
	}
	return jobs.Job{}, "", "", jobs.ErrNotFound
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
	if repo.Status == "superseded" || repo.Status == "disabled" || repo.Status == "missing" {
		writeError(w, r, http.StatusBadRequest, "repository_status_not_operable", "repository status "+repo.Status+" cannot be operated")
		return
	}
	if credentialID != "" {
		if repo.ProviderInstanceID == "" {
			writeError(w, r, http.StatusBadRequest, "credential_provider_instance_required", "repository provider_instance_id is required for credential validation")
			return
		}
		if err := h.store.ValidateCredentialForProtocol(r.Context(), credentialID, repo.ProviderInstanceID, repository.UsageGitTransport, repo.AuthType); err != nil {
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

func (h *RepositoryHandler) cloneRootPreview(r *http.Request, req repository.CloneRequest) (cloneRootSelection, error) {
	if req.RootPathID != "" {
		root, err := h.scannerStore.GetRootPath(r.Context(), req.RootPathID)
		if err != nil {
			return cloneRootSelection{}, err
		}
		return cloneRootSelection{root: root, existing: true, reserveKey: root.ID}, nil
	}
	if req.NewRootPath == "" {
		return cloneRootSelection{}, repository.ValidationError{Code: "invalid_repository_path", Message: "root_path_id or new_root_path is required"}
	}
	normalized, err := repository.NormalizeRootPath(req.NewRootPath)
	if err != nil {
		return cloneRootSelection{}, err
	}
	if root, err := h.scannerStore.RootPathByPath(r.Context(), normalized); err == nil {
		return cloneRootSelection{root: root, existing: true, reserveKey: root.ID}, nil
	} else if !errors.Is(err, scanner.ErrNotFound) {
		return cloneRootSelection{}, err
	}
	return cloneRootSelection{
		root:       scanner.RootPath{Path: normalized},
		existing:   false,
		reserveKey: "root-path:" + normalized,
	}, nil
}

func (h *RepositoryHandler) createCloneRoot(r *http.Request, path string) (scanner.RootPath, error) {
	enabled := true
	items, err := h.scannerStore.UpsertRootPaths(r.Context(), []scanner.RootPathInput{{Path: path, Enabled: &enabled, Source: scanner.RootPathSourceAPI}})
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
	case errors.Is(err, repository.ErrInvalidCursor):
		writeError(w, r, http.StatusBadRequest, "validation_error", "invalid cursor")
	case errors.Is(err, repository.ErrValidation), errors.Is(err, scanner.ErrValidation):
		writeError(w, r, http.StatusBadRequest, repository.ValidationCode(err), err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
	}
}
