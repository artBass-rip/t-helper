package scanner_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/scanner"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestGlobalScanDetectsTerraformProjectsAndMissingLifecycle(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:scanner",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "service", "child", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "ignored", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, ".git", "hidden", "main.tf"), "terraform {}\n")
	linkedTarget := filepath.Join(t.TempDir(), "linked-target")
	mustWriteFile(t, filepath.Join(linkedTarget, "main.tf"), "terraform {}\n")
	if err := os.Symlink(linkedTarget, filepath.Join(rootDir, "linked")); err != nil {
		t.Logf("symlink fixture skipped: %v", err)
	}

	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	if _, err := scannerStore.UpsertIgnoreRules(ctx, []scanner.IgnoreRuleInput{
		{ScopeType: "root_path", ScopeID: roots[0].ID, Pattern: "ignored/", Origin: "ui"},
		{ScopeType: "root_path", ScopeID: roots[0].ID, Pattern: "!ignored/rescue", Origin: "ui"},
	}); err != nil {
		t.Fatalf("upsert ignore rules: %v", err)
	}

	first := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, first.JobID)
	job, err := jobStore.Get(ctx, first.JobID)
	if err != nil {
		t.Fatalf("get first scan: %v", err)
	}
	var result scanner.GlobalScanResult
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		t.Fatalf("decode first scan result: %v", err)
	}
	if result.SchemaVersion != scanner.GlobalScanResultSchema || result.ProjectsCreated != 1 || result.ProjectDiscoveryJobsEnqueued != 1 {
		t.Fatalf("unexpected first scan result: %+v", result)
	}
	if result.DirectoriesSkipped < 2 {
		t.Fatalf("expected ignored and nested directories to be skipped, got %+v", result)
	}
	projects, err := scannerStore.ListProjects(ctx, scanner.ProjectListOptions{Status: "all"})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects.Items) != 1 || projects.Items[0].RelativePath != "service" {
		t.Fatalf("unexpected projects after first scan: %+v", projects.Items)
	}

	if err := os.RemoveAll(filepath.Join(rootDir, "service")); err != nil {
		t.Fatalf("remove service fixture: %v", err)
	}
	second := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, second.JobID)
	active, err := scannerStore.ListProjects(ctx, scanner.ProjectListOptions{})
	if err != nil {
		t.Fatalf("list active projects: %v", err)
	}
	if len(active.Items) != 0 {
		t.Fatalf("missing project should be hidden from default list: %+v", active.Items)
	}
	missing, err := scannerStore.ListProjects(ctx, scanner.ProjectListOptions{Status: scanner.ProjectStatusMissing})
	if err != nil {
		t.Fatalf("list missing projects: %v", err)
	}
	if len(missing.Items) != 1 || missing.Items[0].Status != scanner.ProjectStatusMissing {
		t.Fatalf("expected one missing project: %+v", missing.Items)
	}

	mustWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), "terraform {}\n")
	third := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, third.JobID)
	active, err = scannerStore.ListProjects(ctx, scanner.ProjectListOptions{})
	if err != nil {
		t.Fatalf("list active after rediscovery: %v", err)
	}
	if len(active.Items) != 1 || active.Items[0].Status != scanner.ProjectStatusActive {
		t.Fatalf("expected rediscovered active project: %+v", active.Items)
	}
}

func TestGlobalScanOnlyEnqueuesProjectDiscoveryBeforeRepositoryLinking(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:global-scan-only",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app1", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app2", "main.tf"), "terraform {}\n")

	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}

	ref := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, ref.JobID)

	var repoCount int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM repositories").Scan(&repoCount); err != nil {
		t.Fatalf("count repositories: %v", err)
	}
	if repoCount != 0 {
		t.Fatalf("global scan must not create repository cards directly, got %d", repoCount)
	}
	var discoveryJobs int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE job_type = 'project_discovery' AND parent_job_id = ?", ref.JobID).Scan(&discoveryJobs); err != nil {
		t.Fatalf("count project discovery jobs: %v", err)
	}
	if discoveryJobs != 2 {
		t.Fatalf("project discovery jobs = %d, want 2", discoveryJobs)
	}
}

func TestProjectDiscoveryCreatesGenericRepositoryAndSameRepositoryLink(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:discovery",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app1", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app2", "main.tf"), "terraform {}\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	now := time.Now().UTC()
	app1, _, err := scannerStore.UpsertProject(ctx, roots[0], "repo/app1", now)
	if err != nil {
		t.Fatalf("upsert app1: %v", err)
	}
	app2, _, err := scannerStore.UpsertProject(ctx, roots[0], "repo/app2", now)
	if err != nil {
		t.Fatalf("upsert app2: %v", err)
	}

	ref1 := enqueueProjectDiscovery(t, ctx, jobStore, app1)
	ref2 := enqueueProjectDiscovery(t, ctx, jobStore, app2)
	runUntilComplete(t, ctx, runtime, jobStore, ref1.JobID)
	runUntilComplete(t, ctx, runtime, jobStore, ref2.JobID)

	app1, err = scannerStore.GetProject(ctx, app1.ID)
	if err != nil {
		t.Fatalf("get app1: %v", err)
	}
	app2, err = scannerStore.GetProject(ctx, app2.ID)
	if err != nil {
		t.Fatalf("get app2: %v", err)
	}
	if app1.RepositoryID == "" || app1.RepositoryID != app2.RepositoryID {
		t.Fatalf("projects were not linked to the same repository: app1=%+v app2=%+v", app1, app2)
	}
	var linkCount int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM project_links WHERE repository_id = ?", app1.RepositoryID).Scan(&linkCount); err != nil {
		t.Fatalf("count project links: %v", err)
	}
	if linkCount != 1 {
		t.Fatalf("project links = %d, want 1", linkCount)
	}

	mustWriteFile(t, filepath.Join(rootDir, "negative", ".gitignore"), ".terraform/\n")
	mustWriteFile(t, filepath.Join(rootDir, "negative", "main.tf"), "terraform {}\n")
	negative, _, err := scannerStore.UpsertProject(ctx, roots[0], "negative", now)
	if err != nil {
		t.Fatalf("upsert negative marker project: %v", err)
	}
	negativeRef := enqueueProjectDiscovery(t, ctx, jobStore, negative)
	runUntilComplete(t, ctx, runtime, jobStore, negativeRef.JobID)
	negativeJob, err := jobStore.Get(ctx, negativeRef.JobID)
	if err != nil {
		t.Fatalf("get negative discovery job: %v", err)
	}
	var negativeResult scanner.ProjectDiscoveryResult
	if err := json.Unmarshal(negativeJob.ResultPayload, &negativeResult); err != nil {
		t.Fatalf("decode negative discovery result: %v", err)
	}
	if negativeResult.GitRepositoryDetected {
		t.Fatalf(".gitignore must not be treated as a git marker: %+v", negativeResult)
	}
}

func TestProjectDiscoveryGenericRepositoryIdentityIncludesRootPath(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:generic-root-identity",
		Logger:   slog.Default(),
	})

	rootOne := t.TempDir()
	rootTwo := t.TempDir()
	for _, root := range []string{rootOne, rootTwo} {
		mustWriteFile(t, filepath.Join(root, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
		mustWriteFile(t, filepath.Join(root, "repo", "app", "main.tf"), "terraform {}\n")
	}
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{
		{Name: "one", Path: rootOne, Enabled: &enabled},
		{Name: "two", Path: rootTwo, Enabled: &enabled},
	})
	if err != nil {
		t.Fatalf("upsert root paths: %v", err)
	}
	now := time.Now().UTC()
	projectOne, _, err := scannerStore.UpsertProject(ctx, roots[0], "repo/app", now)
	if err != nil {
		t.Fatalf("upsert project one: %v", err)
	}
	projectTwo, _, err := scannerStore.UpsertProject(ctx, roots[1], "repo/app", now)
	if err != nil {
		t.Fatalf("upsert project two: %v", err)
	}

	resultOne := runProjectDiscoveryResult(t, ctx, runtime, jobStore, projectOne)
	resultTwo := runProjectDiscoveryResult(t, ctx, runtime, jobStore, projectTwo)
	if resultOne.RepositoryID == "" || resultTwo.RepositoryID == "" || resultOne.RepositoryID == resultTwo.RepositoryID {
		t.Fatalf("generic repositories from different roots must remain distinct: one=%+v two=%+v", resultOne, resultTwo)
	}
	var repoCount int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM repositories WHERE provider = ? AND provider_host = ? AND full_path = ?", scanner.RepositoryProviderGeneric, scanner.RepositoryHostLocal, "repo").Scan(&repoCount); err != nil {
		t.Fatalf("count generic repositories: %v", err)
	}
	if repoCount != 2 {
		t.Fatalf("generic repository count = %d, want 2", repoCount)
	}
}

func TestUpsertGenericRepositoryHandlesConcurrentDiscovery(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	repositoryPath := filepath.Join(rootDir, "repo")

	const workers = 16
	start := make(chan struct{})
	errs := make(chan error, workers)
	ids := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			repo, _, _, err := scannerStore.UpsertGenericRepository(ctx, roots[0], repositoryPath)
			if err != nil {
				errs <- err
				return
			}
			ids <- repo.ID
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)

	for err := range errs {
		t.Fatalf("concurrent generic repository upsert failed: %v", err)
	}
	var firstID string
	for id := range ids {
		if firstID == "" {
			firstID = id
			continue
		}
		if id != firstID {
			t.Fatalf("concurrent upsert returned different repository ids: first=%s current=%s", firstID, id)
		}
	}
	if firstID == "" {
		t.Fatal("concurrent upsert did not return any repository ids")
	}
	var repoCount int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM repositories WHERE provider = ? AND provider_host = ? AND root_path_id = ? AND full_path = ?", scanner.RepositoryProviderGeneric, scanner.RepositoryHostLocal, roots[0].ID, "repo").Scan(&repoCount); err != nil {
		t.Fatalf("count generic repositories: %v", err)
	}
	if repoCount != 1 {
		t.Fatalf("generic repository count = %d, want 1", repoCount)
	}
}

func TestProjectDiscoveryGitMarkerAllowlist(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:marker-allowlist",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	now := time.Now().UTC()

	mustWriteFile(t, filepath.Join(rootDir, "worktree", ".git"), "\n  gitdir: ../.git/worktrees/worktree\n")
	mustWriteFile(t, filepath.Join(rootDir, "worktree", "main.tf"), "terraform {}\n")
	worktree, _, err := scannerStore.UpsertProject(ctx, roots[0], "worktree", now)
	if err != nil {
		t.Fatalf("upsert worktree project: %v", err)
	}
	validRef := enqueueProjectDiscovery(t, ctx, jobStore, worktree)
	runUntilComplete(t, ctx, runtime, jobStore, validRef.JobID)
	validJob, err := jobStore.Get(ctx, validRef.JobID)
	if err != nil {
		t.Fatalf("get valid discovery job: %v", err)
	}
	var validResult scanner.ProjectDiscoveryResult
	if err := json.Unmarshal(validJob.ResultPayload, &validResult); err != nil {
		t.Fatalf("decode valid discovery result: %v", err)
	}
	if !validResult.GitRepositoryDetected || validResult.GitMarkerType != ".git_file" || validResult.RepositoryID == "" {
		t.Fatalf("valid .git file was not detected: %+v", validResult)
	}
	repo, err := scannerStore.GetRepository(ctx, validResult.RepositoryID)
	if err != nil {
		t.Fatalf("get discovered repository: %v", err)
	}
	if repo.Provider != scanner.RepositoryProviderGeneric || repo.ProviderHost != scanner.RepositoryHostLocal || repo.CloneURL != "" || repo.IdentityConfirmedAt != nil {
		t.Fatalf("unexpected generic repository card: %+v", repo)
	}

	mustWriteFile(t, filepath.Join(rootDir, "invalid", ".git"), "\nnot-a-gitdir: value\n")
	mustWriteFile(t, filepath.Join(rootDir, "invalid", "main.tf"), "terraform {}\n")
	invalid, _, err := scannerStore.UpsertProject(ctx, roots[0], "invalid", now)
	if err != nil {
		t.Fatalf("upsert invalid marker project: %v", err)
	}
	invalidResult := runProjectDiscoveryResult(t, ctx, runtime, jobStore, invalid)
	if invalidResult.GitRepositoryDetected {
		t.Fatalf("invalid .git file must not be detected: %+v", invalidResult)
	}

	for _, marker := range []string{".gitignore", ".gitattributes", ".gitmodules", ".gitlab-ci.yml", ".github", ".gitkeep"} {
		relative := filepath.ToSlash(filepath.Join("negative-"+strings.Trim(marker, "."), "project"))
		if marker == ".github" {
			mustWriteFile(t, filepath.Join(rootDir, relative, marker, "workflows", "ci.yml"), "name: ci\n")
		} else {
			mustWriteFile(t, filepath.Join(rootDir, relative, marker), "ignored\n")
		}
		mustWriteFile(t, filepath.Join(rootDir, relative, "main.tf"), "terraform {}\n")
		project, _, err := scannerStore.UpsertProject(ctx, roots[0], relative, now)
		if err != nil {
			t.Fatalf("upsert negative marker project %s: %v", marker, err)
		}
		result := runProjectDiscoveryResult(t, ctx, runtime, jobStore, project)
		if result.GitRepositoryDetected {
			t.Fatalf("%s must not be treated as a git marker: %+v", marker, result)
		}
	}
}

func TestProjectDiscoveryRejectsStaleProjectIdentityPayload(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:stale-discovery",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app", "main.tf"), "terraform {}\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	project, _, err := scannerStore.UpsertProject(ctx, roots[0], "repo/app", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	payload, err := json.Marshal(scanner.ProjectDiscoveryPayload{
		SchemaVersion: scanner.ProjectDiscoveryPayloadSchema,
		ProjectID:     project.ID,
		RootPathID:    project.RootPathID,
		RelativePath:  "repo/other",
		Reason:        "test",
	})
	if err != nil {
		t.Fatalf("marshal discovery payload: %v", err)
	}
	ref, err := jobStore.Enqueue(ctx, jobs.EnqueueRequest{JobType: "project_discovery", Payload: payload})
	if err != nil {
		t.Fatalf("enqueue stale discovery job: %v", err)
	}
	job := runUntilTerminal(t, ctx, runtime, jobStore, ref.JobID)
	code := failureErrorCode(t, job)
	if job.Status != jobs.StatusFailed || code != "validation_error" {
		t.Fatalf("stale discovery job = %s/%s, want failed/validation_error", job.Status, code)
	}
}

func TestGlobalScanFailureAndSymlinkPolicy(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:failure-policy",
		Logger:   slog.Default(),
	})

	enabled := true
	missingRoot := filepath.Join(t.TempDir(), "does-not-exist")
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "missing", Path: missingRoot, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert missing root path: %v", err)
	}
	failedRef := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	failedJob := runUntilTerminal(t, ctx, runtime, jobStore, failedRef.JobID)
	failedCode := failureErrorCode(t, failedJob)
	if failedJob.Status != jobs.StatusFailed || failedCode != "global_scan_failed" {
		t.Fatalf("missing root job = %s/%s, want failed/global_scan_failed", failedJob.Status, failedCode)
	}

	targetRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(targetRoot, "service", "main.tf"), "terraform {}\n")
	linkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(targetRoot, linkRoot); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	roots, err = scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "link", Path: linkRoot, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert symlink root path: %v", err)
	}
	linkRef := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	linkJob := runUntilTerminal(t, ctx, runtime, jobStore, linkRef.JobID)
	linkCode := failureErrorCode(t, linkJob)
	if linkJob.Status != jobs.StatusFailed || linkCode != "global_scan_failed" {
		t.Fatalf("symlink root job = %s/%s, want failed/global_scan_failed", linkJob.Status, linkCode)
	}
}

func TestGlobalScanRejectsFollowSymlinksTrue(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:follow-symlinks",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	payload, err := json.Marshal(scanner.GlobalScanPayload{
		SchemaVersion:  scanner.GlobalScanPayloadSchema,
		RootPathIDs:    []string{roots[0].ID},
		Reason:         "test",
		FollowSymlinks: true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if _, err := jobStore.Enqueue(ctx, jobs.EnqueueRequest{JobType: "global_scan", Payload: payload}); err == nil {
		t.Fatal("expected enqueue validation error for follow_symlinks=true")
	}
	_ = runtime
}

func TestGlobalScanPartialDirectoryErrorsStillSucceed(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:partial-errors",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), "terraform {}\n")
	unreadable := filepath.Join(rootDir, "unreadable")
	if err := os.MkdirAll(unreadable, 0o755); err != nil {
		t.Fatalf("mkdir unreadable: %v", err)
	}
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod unreadable: %v", err)
	}
	defer os.Chmod(unreadable, 0o755)

	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	preexisting, _, err := scannerStore.UpsertProject(ctx, roots[0], "unreadable/service", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert preexisting unreadable project: %v", err)
	}
	ref := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, ref.JobID)
	job, err := jobStore.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get scan job: %v", err)
	}
	var result scanner.GlobalScanResult
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.ErrorsCount == 0 {
		t.Skip("filesystem allowed reading chmod 000 directory; partial error assertion is not meaningful in this environment")
	}
	if job.Status != jobs.StatusSucceeded || result.ProjectsCreated != 1 {
		t.Fatalf("partial scan result = status %s result %+v, want succeeded with project and errors", job.Status, result)
	}
	preexisting, err = scannerStore.GetProject(ctx, preexisting.ID)
	if err != nil {
		t.Fatalf("get preexisting unreadable project: %v", err)
	}
	if preexisting.Status != scanner.ProjectStatusActive {
		t.Fatalf("partial scan must not mark projects missing under errored root: %+v", preexisting)
	}
}

func TestGlobalScanPreservesKnownEnvironmentAndWorkspaceLinks(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:link-preservation",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), "terraform {}\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	first := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, first.JobID)
	projects, err := scannerStore.ListProjects(ctx, scanner.ProjectListOptions{})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects.Items) != 1 {
		t.Fatalf("projects = %+v, want one", projects.Items)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := handle.DB.ExecContext(ctx, `INSERT INTO environments (id, name, code, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "env_stage04", "Production", "prod", now, now); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `INSERT INTO workspaces (id, project_id, environment_id, name, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "workspace_stage04", projects.Items[0].ID, "env_stage04", "prod", 1, now, now); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := handle.DB.ExecContext(ctx, `UPDATE projects SET environment_id = ?, default_workspace_id = ? WHERE id = ?`, "env_stage04", "workspace_stage04", projects.Items[0].ID); err != nil {
		t.Fatalf("link project: %v", err)
	}

	second := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, second.JobID)
	project, err := scannerStore.GetProject(ctx, projects.Items[0].ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if project.EnvironmentID != "env_stage04" || project.DefaultWorkspaceID != "workspace_stage04" {
		t.Fatalf("known links were not preserved after scan: %+v", project)
	}
}

func enqueueGlobalScan(t *testing.T, ctx context.Context, store *jobs.Store, rootPathID string) jobs.JobRef {
	t.Helper()
	payload, err := json.Marshal(scanner.GlobalScanPayload{
		SchemaVersion:  scanner.GlobalScanPayloadSchema,
		RootPathIDs:    []string{rootPathID},
		Reason:         "test",
		FollowSymlinks: false,
	})
	if err != nil {
		t.Fatalf("marshal global scan payload: %v", err)
	}
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "global_scan", Payload: payload})
	if err != nil {
		t.Fatalf("enqueue global scan: %v", err)
	}
	return ref
}

func runProjectDiscoveryResult(t *testing.T, ctx context.Context, runtime *jobs.Runtime, store *jobs.Store, project scanner.Project) scanner.ProjectDiscoveryResult {
	t.Helper()
	ref := enqueueProjectDiscovery(t, ctx, store, project)
	runUntilComplete(t, ctx, runtime, store, ref.JobID)
	job, err := store.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get discovery job: %v", err)
	}
	var result scanner.ProjectDiscoveryResult
	if err := json.Unmarshal(job.ResultPayload, &result); err != nil {
		t.Fatalf("decode discovery result: %v", err)
	}
	return result
}

func enqueueProjectDiscovery(t *testing.T, ctx context.Context, store *jobs.Store, project scanner.Project) jobs.JobRef {
	t.Helper()
	payload, err := json.Marshal(scanner.ProjectDiscoveryPayload{
		SchemaVersion: scanner.ProjectDiscoveryPayloadSchema,
		ProjectID:     project.ID,
		RootPathID:    project.RootPathID,
		RelativePath:  project.RelativePath,
		Reason:        "test",
	})
	if err != nil {
		t.Fatalf("marshal discovery payload: %v", err)
	}
	ref, err := store.Enqueue(ctx, jobs.EnqueueRequest{JobType: "project_discovery", Payload: payload})
	if err != nil {
		t.Fatalf("enqueue project discovery: %v", err)
	}
	return ref
}

func runUntilComplete(t *testing.T, ctx context.Context, runtime *jobs.Runtime, store *jobs.Store, jobID string) {
	t.Helper()
	job := runUntilTerminal(t, ctx, runtime, store, jobID)
	if job.Status != jobs.StatusSucceeded {
		t.Fatalf("job %s finished with %s: %s", jobID, job.Status, job.ErrorMessage)
	}
}

func runUntilTerminal(t *testing.T, ctx context.Context, runtime *jobs.Runtime, store *jobs.Store, jobID string) jobs.Job {
	t.Helper()
	for i := 0; i < 20; i++ {
		job, err := store.Get(ctx, jobID)
		if err != nil {
			t.Fatalf("get job %s: %v", jobID, err)
		}
		if job.Status == jobs.StatusSucceeded || job.Status == jobs.StatusFailed || job.Status == jobs.StatusCancelled {
			return job
		}
		if _, err := runtime.RunOnce(ctx); err != nil {
			t.Fatalf("runtime run once: %v", err)
		}
	}
	t.Fatalf("job %s did not complete", jobID)
	return jobs.Job{}
}

func failureErrorCode(t *testing.T, job jobs.Job) string {
	t.Helper()
	var failure jobs.FailureResult
	if err := json.Unmarshal(job.ResultPayload, &failure); err != nil {
		t.Fatalf("decode failure result for job %s: %v", job.ID, err)
	}
	return failure.ErrorCode
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func openMigratedSQLite(t *testing.T) *storage.Handle {
	t.Helper()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(context.Background(), storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "scanner.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := provider.Migrate(context.Background(), handle); err != nil {
		handle.Close()
		t.Fatalf("migrate sqlite: %v", err)
	}
	return handle
}
