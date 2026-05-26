package repository

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"

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
	if _, err := os.Stat(localPath); err == nil {
		if _, statErr := os.Stat(localPath + string(os.PathSeparator) + ".git"); statErr == nil {
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
