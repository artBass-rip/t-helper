package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
		result, err := h.handleClone(ctx, job)
		return result, h.recordRepositoryFailure(ctx, job, err)
	case "repo_pull":
		result, err := h.handlePull(ctx, job)
		return result, h.recordRepositoryFailure(ctx, job, err)
	case "repo_sync":
		result, err := h.handlePull(ctx, job)
		return result, h.recordRepositoryFailure(ctx, job, err)
	default:
		return nil, jobs.HandlerError{Code: "validation_error", Message: "unsupported repository operation", Retryable: false}
	}
}

func (h operationHandler) recordRepositoryFailure(ctx context.Context, job jobs.Job, err error) error {
	if err == nil {
		return nil
	}
	repositoryID := repositoryIDFromPayload(job.Payload)
	if repositoryID == "" {
		return err
	}
	message := err.Error()
	var handlerErr jobs.HandlerError
	if errors.As(err, &handlerErr) && handlerErr.Code != "" {
		message = handlerErr.Code + ": " + handlerErr.Message
	}
	_ = h.store.MarkRepositoryError(ctx, repositoryID, redactRepositoryMessage(message))
	return err
}

func repositoryIDFromPayload(raw json.RawMessage) string {
	var payload struct {
		RepositoryID string `json:"repository_id"`
	}
	_ = json.Unmarshal(raw, &payload)
	return strings.TrimSpace(payload.RepositoryID)
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
		if err := h.store.ValidateCredentialForProtocol(ctx, payload.CredentialID, payload.ProviderInstanceID, UsageGitTransport, payload.Protocol); err != nil {
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
	identityReservationKey := IdentityReservationKey(payload.Provider, payload.ProviderHost, payload.FullPath)
	pathReservationKey, err := TargetReservationKey(root.Path, payload.RootPathID, payload.TargetDirectory)
	if err != nil {
		return nil, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
	}
	held, err := h.store.ReserveOperationKeys(ctx, job.ID, time.Hour, identityReservationKey, pathReservationKey)
	if err != nil {
		if errors.Is(err, ErrReservationConflict) {
			code := "repository_target_path_busy"
			var conflict ReservationConflictError
			if errors.As(err, &conflict) && conflict.Key == identityReservationKey {
				code = "repository_operation_already_running"
			}
			return nil, jobs.HandlerError{Code: code, Message: err.Error(), Retryable: true}
		}
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	defer h.store.ReleaseOperationReservations(ctx, job.ID, held...)
	if _, err := os.Stat(localPath); err == nil {
		if _, statErr := os.Stat(localPath + string(os.PathSeparator) + ".git"); statErr == nil {
			if err := h.validateExistingRemote(ctx, localPath, payload); err != nil {
				return nil, err
			}
			return h.runGit(ctx, repo, payload.CredentialID, job.JobType, localPath, gitEnv, "pull", "--ff-only")
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
	cmd.Env = gitCommandEnv(gitEnv)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, jobs.HandlerError{Code: "git_clone_failed", Message: redactRepositoryMessage(string(out)), Retryable: true}
	}
	defaultBranch := gitDefaultBranch(ctx, localPath)
	if err := h.store.TouchRepositoryPulledWithBranch(ctx, payload.RepositoryID, defaultBranch); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	afterRevision, _ := gitRevision(ctx, localPath)
	return json.Marshal(OperationResult{
		SchemaVersion:       RepoOperationResultSchema,
		RepositoryID:        payload.RepositoryID,
		ProviderInstanceID:  payload.ProviderInstanceID,
		CredentialID:        payload.CredentialID,
		Operation:           job.JobType,
		RootPathID:          payload.RootPathID,
		Provider:            payload.Provider,
		ProviderHost:        payload.ProviderHost,
		Protocol:            payload.Protocol,
		LocalPath:           localPath,
		RepositoriesCreated: boolInt(payload.RepositoryCreated),
		AfterRevision:       afterRevision,
		Changed:             true,
		ExitCode:            0,
	})
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
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
		payload.CredentialID = syncPayload.CredentialID
	}
	repo, err := h.scannerStore.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if err := h.validateRepositoryOperable(repo); err != nil {
		return nil, err
	}
	if payload.CredentialID != "" {
		if err := h.store.ValidateCredentialForProtocol(ctx, payload.CredentialID, repo.ProviderInstanceID, UsageGitTransport, repo.AuthType); err != nil {
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
		if err := h.store.ValidateCredentialForProtocol(ctx, credentialID, repo.ProviderInstanceID, UsageGitTransport, repo.AuthType); err != nil {
			return nil, jobs.HandlerError{Code: ValidationCode(err), Message: err.Error(), Retryable: false}
		}
	}
	gitEnv, cleanup, err := h.gitCredentialEnv(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return h.runGit(ctx, repo, credentialID, job.JobType, localPath, gitEnv, "pull", "--ff-only")
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

func (h operationHandler) runGit(ctx context.Context, repo scanner.Repository, credentialID, operation, dir string, extraEnv []string, args ...string) (json.RawMessage, error) {
	beforeRevision, _ := gitRevision(ctx, dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitCommandEnv(extraEnv)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, jobs.HandlerError{Code: "git_operation_failed", Message: redactRepositoryMessage(string(out)), Retryable: true}
	}
	defaultBranch := gitDefaultBranch(ctx, dir)
	if err := h.store.TouchRepositoryPulledWithBranch(ctx, repo.ID, defaultBranch); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	afterRevision, _ := gitRevision(ctx, dir)
	var before *string
	if beforeRevision != "" {
		before = &beforeRevision
	}
	return json.Marshal(OperationResult{
		SchemaVersion:      RepoOperationResultSchema,
		RepositoryID:       repo.ID,
		ProviderInstanceID: repo.ProviderInstanceID,
		CredentialID:       credentialID,
		Operation:          operation,
		RootPathID:         repo.RootPathID,
		Provider:           repo.Provider,
		ProviderHost:       repo.ProviderHost,
		Protocol:           repo.AuthType,
		LocalPath:          dir,
		BeforeRevision:     before,
		AfterRevision:      afterRevision,
		Changed:            beforeRevision != afterRevision,
		ExitCode:           0,
	})
}

func gitCommandEnv(extra []string) []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	return append(env, extra...)
}

var repositorySecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization|bearer|token|password|passwd|pwd|secret|private[_ -]?key)\s*[:=]\s*[^,\s]+`),
	regexp.MustCompile(`(?i)https?://[^/\s:@]+:[^/\s@]+@`),
	regexp.MustCompile(`(?i)secretref://[^\s,]+`),
}

func redactRepositoryMessage(value string) string {
	for _, pattern := range repositorySecretPatterns {
		value = pattern.ReplaceAllStringFunc(value, func(match string) string {
			if strings.Contains(match, "://") && strings.Contains(match, "@") {
				parts := strings.SplitN(match, "://", 2)
				return parts[0] + "://[redacted]@"
			}
			if idx := strings.IndexAny(match, ":="); idx >= 0 {
				return strings.TrimSpace(match[:idx]) + match[idx:idx+1] + "[redacted]"
			}
			return "[redacted]"
		})
	}
	return strings.TrimSpace(value)
}

func gitRevision(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitDefaultBranch(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return strings.TrimPrefix(branch, "origin/")
		}
	}
	cmd = exec.CommandContext(ctx, "git", "branch", "--show-current")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
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
