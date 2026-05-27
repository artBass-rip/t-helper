package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/scanner"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestUpsertRepositoryEnrichesGenericRepositoryInPlace(t *testing.T) {
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	localPath := filepath.Join(root.Path, "repo")
	generic, _, _, err := scannerStore.UpsertGenericRepository(ctx, root, localPath)
	if err != nil {
		t.Fatalf("upsert generic repository: %v", err)
	}
	project, _, err := scannerStore.UpsertProject(ctx, root, "repo/app", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	if err := scannerStore.SetProjectRepository(ctx, project.ID, generic.ID); err != nil {
		t.Fatalf("link project: %v", err)
	}

	enriched, err := repoStore.UpsertRepository(ctx, Identity{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/repo",
		CloneURL:     "git@github.com:example/repo.git",
		Protocol:     ProtocolSSH,
	}, root, "repo", localPath, "", "")
	if err != nil {
		t.Fatalf("enrich repository: %v", err)
	}
	if enriched.ID != generic.ID {
		t.Fatalf("enriched id = %q, want original generic id %q", enriched.ID, generic.ID)
	}
	if enriched.Provider != ProviderGitHub || enriched.ProviderHost != "github.com" || enriched.FullPath != "example/repo" || enriched.IdentityConfirmedAt == nil {
		t.Fatalf("unexpected enriched repository: %+v", enriched)
	}
	reloadedProject, err := scannerStore.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if reloadedProject.RepositoryID != generic.ID {
		t.Fatalf("project repository_id = %q, want %q", reloadedProject.RepositoryID, generic.ID)
	}
}

func TestUpsertRepositoryRelinksAndSupersedesGenericRepository(t *testing.T) {
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)

	canonical, err := repoStore.UpsertRepository(ctx, Identity{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/repo",
		CloneURL:     "https://github.com/example/repo.git",
		Protocol:     ProtocolHTTPS,
	}, root, "managed-repo", filepath.Join(root.Path, "managed-repo"), "", "")
	if err != nil {
		t.Fatalf("upsert canonical repository: %v", err)
	}
	generic, _, _, err := scannerStore.UpsertGenericRepository(ctx, root, filepath.Join(root.Path, "repo"))
	if err != nil {
		t.Fatalf("upsert generic repository: %v", err)
	}
	app, _, err := scannerStore.UpsertProject(ctx, root, "repo/app", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert app project: %v", err)
	}
	other, _, err := scannerStore.UpsertProject(ctx, root, "repo/other", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert other project: %v", err)
	}
	if err := scannerStore.SetProjectRepository(ctx, app.ID, generic.ID); err != nil {
		t.Fatalf("link app project: %v", err)
	}
	if err := scannerStore.SetProjectRepository(ctx, other.ID, generic.ID); err != nil {
		t.Fatalf("link other project: %v", err)
	}
	if _, err := scannerStore.UpsertProjectLink(ctx, app.ID, other.ID, generic.ID, ""); err != nil {
		t.Fatalf("upsert project link: %v", err)
	}

	updated, err := repoStore.UpsertRepository(ctx, Identity{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/repo",
		CloneURL:     "git@github.com:example/repo.git",
		Protocol:     ProtocolSSH,
	}, root, "repo", filepath.Join(root.Path, "repo"), "", "")
	if err != nil {
		t.Fatalf("relink repository: %v", err)
	}
	if updated.ID != canonical.ID {
		t.Fatalf("updated id = %q, want canonical id %q", updated.ID, canonical.ID)
	}
	reloadedGeneric, err := scannerStore.GetRepository(ctx, generic.ID)
	if err != nil {
		t.Fatalf("reload generic: %v", err)
	}
	if reloadedGeneric.Status != "superseded" || reloadedGeneric.SupersededByRepositoryID != canonical.ID {
		t.Fatalf("generic repository was not superseded: %+v", reloadedGeneric)
	}
	projects, err := scannerStore.ProjectsByRepository(ctx, canonical.ID)
	if err != nil {
		t.Fatalf("projects by canonical: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("canonical project links = %d, want 2: %+v", len(projects), projects)
	}
	links, err := scannerStore.ListProjectLinks(ctx, scanner.ProjectLinkListOptions{RepositoryID: canonical.ID})
	if err != nil {
		t.Fatalf("list project links: %v", err)
	}
	if len(links.Items) != 1 {
		t.Fatalf("canonical project link count = %d, want 1", len(links.Items))
	}
}

func TestReserveOperationKeysReportsConflict(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	held, err := store.ReserveOperationKeys(ctx, "owner-one", time.Minute, "repository-path:root:repo")
	if err != nil {
		t.Fatalf("reserve first: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("held reservations = %d, want 1", len(held))
	}
	_, err = store.ReserveOperationKeys(ctx, "owner-two", time.Minute, "repository-path:root:repo")
	if !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("reserve second error = %v, want reservation conflict", err)
	}
	if err := store.ReleaseOperationReservations(ctx, "owner-one", held...); err != nil {
		t.Fatalf("release first: %v", err)
	}
	if _, err := store.ReserveOperationKeys(ctx, "owner-two", time.Minute, "repository-path:root:repo"); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
}

func TestTransferOperationReservationsAllowsSameJobRefresh(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	held, err := store.ReserveOperationKeys(ctx, "request-one", time.Minute, "repository-identity:github:github.com:example/repo")
	if err != nil {
		t.Fatalf("reserve request: %v", err)
	}
	if err := store.TransferOperationReservations(ctx, "request-one", "job-one", time.Hour, held...); err != nil {
		t.Fatalf("transfer reservation: %v", err)
	}
	if _, err := store.ReserveOperationKeys(ctx, "job-one", time.Hour, held...); err != nil {
		t.Fatalf("same job should refresh reservation: %v", err)
	}
	if _, err := store.ReserveOperationKeys(ctx, "job-two", time.Hour, held...); !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("second job reserve error = %v, want reservation conflict", err)
	}
}

func TestCredentialValidationUsesADRAuthTypesAndUsages(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	enabled := true
	instances, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		Name:         "GitHub",
		Enabled:      &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	credentials, err := store.UpsertCredentials(ctx, []CredentialInput{{
		ProviderInstanceID: instances[0].ID,
		Name:               "read-only",
		AuthType:           AuthTypeHTTPSToken,
		SecretRef:          "secretref://env/GITHUB_TOKEN",
		Usages:             []string{UsageGitTransport, UsageWebhook},
		Enabled:            &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
	if err := store.ValidateCredential(ctx, credentials[0].ID, instances[0].ID, UsageGitTransport); err != nil {
		t.Fatalf("validate git transport credential: %v", err)
	}
	if err := store.ValidateCredential(ctx, credentials[0].ID, instances[0].ID, UsageProviderAPI); err == nil {
		t.Fatal("expected provider_api usage validation error")
	}
	listed, err := store.ListCredentials(ctx, CredentialListOptions{ProviderInstanceID: instances[0].ID, Usage: UsageWebhook})
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	if len(listed) != 1 || listed[0].SecretRef != "secretref://env/***" {
		t.Fatalf("unexpected listed credentials: %+v", listed)
	}
}

func TestProviderInstanceNormalizesDefaultHTTPSPort(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	instances, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com:443",
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	if instances[0].ProviderHost != "github.com" {
		t.Fatalf("provider_host = %q, want github.com", instances[0].ProviderHost)
	}
}

func TestGitCredentialEnvResolvesSecretRef(t *testing.T) {
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	store := NewStore(handle)
	scannerStore := scanner.NewStore(handle)
	enabled := true
	instances, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		Enabled:      &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	credentials, err := store.UpsertCredentials(ctx, []CredentialInput{{
		ProviderInstanceID: instances[0].ID,
		Name:               "token",
		AuthType:           AuthTypeHTTPSToken,
		SecretRef:          "secretref://env/REPOSITORY_TOKEN",
		Usages:             []string{UsageGitTransport},
		Enabled:            &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
	t.Setenv("REPOSITORY_TOKEN", "secret-token")
	env, cleanup, err := (operationHandler{store: store, scannerStore: scannerStore}).gitCredentialEnv(ctx, credentials[0].ID)
	defer cleanup()
	if err != nil {
		t.Fatalf("credential env: %v", err)
	}
	if len(env) != 3 || env[0] != "GIT_CONFIG_COUNT=1" || env[1] != "GIT_CONFIG_KEY_0=http.extraHeader" {
		t.Fatalf("unexpected credential env: %+v", env)
	}
}

func TestOperationHandlerCloneRunsGitAndCreatesWorkingTree(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	source := filepath.Join(t.TempDir(), "source")
	runGitCommand(t, "", "init", source)
	mustWriteRepositoryTestFile(t, filepath.Join(source, "README.md"), "fixture\n")
	runGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "add", "README.md")
	runGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	targetDirectory := "cloned/repo"
	localPath := filepath.Join(root.Path, "cloned", "repo")
	repo, err := repoStore.UpsertRepository(ctx, Identity{
		Provider:     ProviderGeneric,
		ProviderHost: "local",
		FullPath:     "source",
		CloneURL:     source,
		Protocol:     ProtocolHTTPS,
	}, root, targetDirectory, localPath, "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	payload, _ := json.Marshal(RepoClonePayload{
		SchemaVersion:   RepoClonePayloadSchema,
		RepositoryID:    repo.ID,
		Provider:        ProviderGeneric,
		ProviderHost:    "local",
		Protocol:        ProtocolHTTPS,
		CloneURL:        source,
		CloneScope:      "single_repository",
		FullPath:        "source",
		RootPathID:      root.ID,
		TargetDirectory: targetDirectory,
		LocalPath:       localPath,
	})
	result, err := (operationHandler{store: repoStore, scannerStore: scannerStore, operation: "clone"}).handleClone(ctx, jobs.Job{
		ID:      "job_clone_git",
		JobType: "repo_clone",
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("handle clone: %v", err)
	}
	var op OperationResult
	if err := json.Unmarshal(result, &op); err != nil {
		t.Fatalf("decode operation result: %v", err)
	}
	if op.SchemaVersion != RepoOperationResultSchema || op.Operation != "repo_clone" || op.RepositoryID != repo.ID || op.Provider != ProviderGeneric || op.ProviderHost != "local" || op.Protocol != ProtocolHTTPS || op.LocalPath != localPath || op.RepositoriesCreated != 0 || !op.Changed || op.AfterRevision == "" {
		t.Fatalf("unexpected operation result: %+v", op)
	}
	if _, err := os.Stat(filepath.Join(localPath, ".git")); err != nil {
		t.Fatalf("cloned working tree missing .git: %v", err)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func mustWriteRepositoryTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func upsertRepositoryRoot(t *testing.T, ctx context.Context, store *scanner.Store) scanner.RootPath {
	t.Helper()
	enabled := true
	items, err := store.UpsertRootPaths(ctx, []scanner.RootPathInput{{Path: filepath.Join(t.TempDir(), "root"), Enabled: &enabled, Source: scanner.RootPathSourceAPI}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	return items[0]
}

func openRepositorySQLite(t *testing.T) *storage.Handle {
	t.Helper()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(context.Background(), storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "repository.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := provider.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return handle
}
