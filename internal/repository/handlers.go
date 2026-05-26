package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	repo, err := h.scannerStore.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if err := h.validateRepositoryOperable(repo); err != nil {
		return nil, err
	}
	if payload.CredentialID != "" {
		if err := h.store.ValidateCredential(ctx, payload.CredentialID, payload.ProviderInstanceID, UsageGitTransport); err != nil {
			return nil, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
		}
	}
	gitEnv, cleanup, err := h.gitCredentialEnv(ctx, payload.CredentialID)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	_, localPath, err := NormalizeTarget(root.Path, payload.TargetDirectory)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if payload.LocalPath != "" && filepath.Clean(payload.LocalPath) != filepath.Clean(localPath) {
		return nil, jobs.HandlerError{Code: "invalid_repository_path", Message: "repository local_path no longer matches target_directory", Retryable: false}
	}
	pathReservationKey, err := TargetReservationKey(root.Path, payload.RootPathID, payload.TargetDirectory)
	if err != nil {
		return nil, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
	}
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
			return h.runGit(ctx, payload.RepositoryID, localPath, gitEnv, "pull", "--ff-only")
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
	cmd.Env = append(os.Environ(), gitEnv...)
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
	if err := h.validateRepositoryOperable(repo); err != nil {
		return nil, err
	}
	if payload.CredentialID != "" {
		if err := h.store.ValidateCredential(ctx, payload.CredentialID, repo.ProviderInstanceID, UsageGitTransport); err != nil {
			return nil, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
		}
	}
	if repo.LocalPath == "" {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "repository local_path is required", Retryable: false}
	}
	localPath, err := h.validatedRepositoryLocalPath(ctx, repo)
	if err != nil {
		return nil, err
	}
	if repo.Provider != ProviderGeneric || repo.ProviderHost != "local" {
		if err := h.validateExistingRemote(ctx, localPath, RepoClonePayload{Provider: repo.Provider, ProviderHost: repo.ProviderHost, FullPath: repo.FullPath}); err != nil {
			return nil, err
		}
	}
	credentialID := payload.CredentialID
	if credentialID == "" {
		credentialID = repo.DefaultCredentialID
	}
	if credentialID != "" {
		if err := h.store.ValidateCredential(ctx, credentialID, repo.ProviderInstanceID, UsageGitTransport); err != nil {
			return nil, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
		}
	}
	gitEnv, cleanup, err := h.gitCredentialEnv(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return h.runGit(ctx, repo.ID, localPath, gitEnv, "pull", "--ff-only")
}

func (h operationHandler) validateRepositoryOperable(repo scanner.Repository) error {
	switch repo.Status {
	case "active":
		return nil
	case "superseded", "disabled", "missing":
		return jobs.HandlerError{Code: "repository_status_not_operable", Message: "repository status " + repo.Status + " cannot be operated", Retryable: false}
	default:
		return jobs.HandlerError{Code: "validation_error", Message: "repository status " + repo.Status + " cannot be operated", Retryable: false}
	}
}

func (h operationHandler) validatedRepositoryLocalPath(ctx context.Context, repo scanner.Repository) (string, error) {
	if repo.RootPathID == "" || repo.TargetDirectory == "" {
		return "", jobs.HandlerError{Code: "invalid_repository_path", Message: "repository root_path_id and target_directory are required", Retryable: false}
	}
	root, err := h.scannerStore.GetRootPath(ctx, repo.RootPathID)
	if err != nil {
		return "", jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	_, localPath, err := NormalizeTarget(root.Path, repo.TargetDirectory)
	if err != nil {
		return "", jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
	}
	if filepath.Clean(repo.LocalPath) != filepath.Clean(localPath) {
		return "", jobs.HandlerError{Code: "invalid_repository_path", Message: "repository local_path no longer matches target_directory", Retryable: false}
	}
	return localPath, nil
}

func (h operationHandler) runGit(ctx context.Context, repositoryID, dir string, extraEnv []string, args ...string) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, jobs.HandlerError{Code: "git_operation_failed", Message: string(out), Retryable: true}
	}
	if err := h.store.TouchRepositoryPulled(ctx, repositoryID); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	return json.Marshal(OperationResult{SchemaVersion: RepoOperationResultSchema, RepositoryID: repositoryID, Operation: h.operation})
}

func (h operationHandler) gitCredentialEnv(ctx context.Context, credentialID string) ([]string, func(), error) {
	cleanup := func() {}
	if strings.TrimSpace(credentialID) == "" {
		return nil, cleanup, nil
	}
	cred, err := h.store.GetCredential(ctx, credentialID)
	if err != nil {
		return nil, cleanup, jobs.HandlerError{Code: "credential_not_found", Message: err.Error(), Retryable: false}
	}
	secret, err := resolveEnvSecret(cred.SecretRef)
	if err != nil {
		return nil, cleanup, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
	}
	switch cred.AuthType {
	case AuthTypeHTTPSToken, AuthTypeHTTPSBasic, AuthTypeAppPassword:
		username := cred.Username
		if username == "" {
			username = "x-access-token"
		}
		token := base64.StdEncoding.EncodeToString([]byte(username + ":" + secret))
		return []string{
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic " + token,
		}, cleanup, nil
	case AuthTypeOAuthToken:
		return []string{
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Bearer " + secret,
		}, cleanup, nil
	case AuthTypeSSHKey:
		file, err := os.CreateTemp("", "thelper-git-key-*")
		if err != nil {
			return nil, cleanup, jobs.HandlerError{Code: "credential_unavailable", Message: err.Error(), Retryable: true}
		}
		path := file.Name()
		cleanup = func() { _ = os.Remove(path) }
		if _, err := file.WriteString(secret); err != nil {
			_ = file.Close()
			cleanup()
			return nil, func() {}, jobs.HandlerError{Code: "credential_unavailable", Message: err.Error(), Retryable: true}
		}
		if err := file.Close(); err != nil {
			cleanup()
			return nil, func() {}, jobs.HandlerError{Code: "credential_unavailable", Message: err.Error(), Retryable: true}
		}
		if err := os.Chmod(path, 0o600); err != nil {
			cleanup()
			return nil, func() {}, jobs.HandlerError{Code: "credential_unavailable", Message: err.Error(), Retryable: true}
		}
		return []string{"GIT_SSH_COMMAND=ssh -i " + path + " -o IdentitiesOnly=yes"}, cleanup, nil
	default:
		return nil, cleanup, jobs.HandlerError{Code: "unsupported_credential_auth_type", Message: "unsupported credential auth_type", Retryable: false}
	}
}

func resolveEnvSecret(secretRef string) (string, error) {
	const prefix = "secretref://env/"
	if !strings.HasPrefix(secretRef, prefix) {
		return "", validationError("invalid_secret_ref", "secret_ref must use secretref://env/NAME")
	}
	name := strings.TrimPrefix(secretRef, prefix)
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return "", validationError("secret_ref_unresolved", "secret_ref environment variable is not set")
	}
	return value, nil
}
