package scanner_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
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
	for i := 0; i < 20; i++ {
		job, err := store.Get(ctx, jobID)
		if err != nil {
			t.Fatalf("get job %s: %v", jobID, err)
		}
		if job.Status == jobs.StatusSucceeded || job.Status == jobs.StatusFailed || job.Status == jobs.StatusCancelled {
			if job.Status != jobs.StatusSucceeded {
				t.Fatalf("job %s finished with %s: %s", jobID, job.Status, job.ErrorMessage)
			}
			return
		}
		if _, err := runtime.RunOnce(ctx); err != nil {
			t.Fatalf("runtime run once: %v", err)
		}
	}
	t.Fatalf("job %s did not complete", jobID)
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
