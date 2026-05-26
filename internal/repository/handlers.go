package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/scanner"
)

type operationHandler struct {
	store        *Store
	scannerStore *scanner.Store
	operation    string
}

func JobHandlers(store *Store, scannerStore *scanner.Store) map[string]jobs.Handler {
	return map[string]jobs.Handler{
		"repo_clone": operationHandler{store: store, scannerStore: scannerStore, operation: "clone"},
		"repo_pull":  operationHandler{store: store, scannerStore: scannerStore, operation: "pull"},
		"repo_sync":  operationHandler{store: store, scannerStore: scannerStore, operation: "sync"},
	}
}

func (h operationHandler) Handle(ctx context.Context, env jobs.HandlerEnv, job jobs.Job) (json.RawMessage, error) {
	switch job.JobType {
	case "repo_clone":
		return h.handleClone(ctx, job)
	case "repo_pull":
		return h.handlePull(ctx, job)
	case "repo_sync":
		return h.handlePull(ctx, job)
	default:
		return nil, jobs.HandlerError{Code: "validation_error", Message: "unsupported repository operation", Retryable: false}
	}
}

func (h operationHandler) handleClone(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload RepoClonePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	root, err := h.scannerStore.GetRootPath(ctx, payload.RootPathID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	_, localPath, err := NormalizeTarget(root.Path, payload.TargetDirectory)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	pathReservationKey := "repository-path:" + payload.RootPathID + ":" + payload.TargetDirectory
	held, err := h.store.ReserveOperationKeys(ctx, job.ID, time.Hour, pathReservationKey)
	if err != nil {
		if errors.Is(err, ErrReservationConflict) {
			return nil, jobs.HandlerError{Code: "repository_target_path_busy", Message: pathReservationKey, Retryable: true}
		}
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	defer h.store.ReleaseOperationReservations(ctx, job.ID, held...)
	if _, err := os.Stat(localPath); err == nil {
		if _, statErr := os.Stat(localPath + string(os.PathSeparator) + ".git"); statErr == nil {
			if err := h.validateExistingRemote(ctx, localPath, payload); err != nil {
				return nil, err
			}
			return h.runGit(ctx, payload.RepositoryID, localPath, "pull", "--ff-only")
		}
		entries, readErr := os.ReadDir(localPath)
		if readErr != nil {
			return nil, jobs.HandlerError{Code: "repository_target_unavailable", Message: readErr.Error(), Retryable: true}
		}
		if len(entries) > 0 {
			return nil, jobs.HandlerError{Code: "repository_target_not_empty", Message: "target directory is not empty", Retryable: false}
		}
	} else if os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(localPath, 0o755); mkdirErr != nil {
			return nil, jobs.HandlerError{Code: "repository_target_unavailable", Message: mkdirErr.Error(), Retryable: true}
		}
	} else {
		return nil, jobs.HandlerError{Code: "repository_target_unavailable", Message: err.Error(), Retryable: true}
	}
	cmd := exec.CommandContext(ctx, "git", "clone", payload.CloneURL, localPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, jobs.HandlerError{Code: "git_clone_failed", Message: string(out), Retryable: true}
	}
	if err := h.store.TouchRepositoryPulled(ctx, payload.RepositoryID); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	return json.Marshal(OperationResult{SchemaVersion: RepoOperationResultSchema, RepositoryID: payload.RepositoryID, Operation: "clone"})
}

func (h operationHandler) validateExistingRemote(ctx context.Context, localPath string, payload RepoClonePayload) error {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = localPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return jobs.HandlerError{Code: "repository_remote_unavailable", Message: strings.TrimSpace(string(out)), Retryable: false}
	}
	remote := strings.TrimSpace(string(out))
	identity, err := ParseCloneURL(payload.Provider, remote)
	if err != nil {
		return jobs.HandlerError{Code: "repository_remote_mismatch", Message: "existing repository remote cannot be normalized", Retryable: false}
	}
	if identity.ProviderHost != payload.ProviderHost || identity.FullPath != payload.FullPath {
		return jobs.HandlerError{Code: "repository_remote_mismatch", Message: "existing repository remote does not match requested repository", Retryable: false}
	}
	return nil
}

func (h operationHandler) handlePull(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload RepoPullPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		var syncPayload RepoSyncPayload
		if syncErr := json.Unmarshal(job.Payload, &syncPayload); syncErr != nil {
			return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
		}
		payload.RepositoryID = syncPayload.RepositoryID
	}
	repo, err := h.scannerStore.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if repo.LocalPath == "" {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "repository local_path is required", Retryable: false}
	}
	return h.runGit(ctx, repo.ID, repo.LocalPath, "pull", "--ff-only")
}

func (h operationHandler) runGit(ctx context.Context, repositoryID, dir string, args ...string) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, jobs.HandlerError{Code: "git_operation_failed", Message: string(out), Retryable: true}
	}
	if err := h.store.TouchRepositoryPulled(ctx, repositoryID); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	return json.Marshal(OperationResult{SchemaVersion: RepoOperationResultSchema, RepositoryID: repositoryID, Operation: h.operation})
}
