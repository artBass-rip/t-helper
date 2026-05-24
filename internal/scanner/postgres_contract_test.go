package scanner_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/scanner"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestStage04ScannerContractPostgres(t *testing.T) {
	dsn := os.Getenv("THELPER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("THELPER_POSTGRES_DSN is not set")
	}
	requirePostgresScannerTestDatabase(t, dsn)

	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	handle, err := registry.Open(ctx, storage.Config{Provider: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer handle.Close()
	resetScannerPostgresTables(t, handle.DB)
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}

	scannerStore := scanner.NewStore(handle)
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:    jobStore,
		Handlers: scanner.JobHandlers(scannerStore),
		WorkerID: "host:test:stage04-postgres",
	})

	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "repo", ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app1", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "app2", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "repo", "ignored", "main.tf"), "terraform {}\n")
	mustWriteFile(t, filepath.Join(rootDir, "file-ignore", "main.tf"), "terraform {}\n")

	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []scanner.RootPathInput{{Name: "postgres-root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	if _, err := scannerStore.UpsertIgnoreRules(ctx, []scanner.IgnoreRuleInput{
		{ScopeType: "root_path", ScopeID: roots[0].ID, Pattern: "repo/ignored/", Origin: "ui"},
		{ScopeType: "root_path", ScopeID: roots[0].ID, Pattern: "file-ignore/main.tf", Origin: "ui"},
		{ScopeType: "root_path", ScopeID: roots[0].ID, Pattern: "!file-ignore/main.tf", Origin: "ui"},
	}); err != nil {
		t.Fatalf("upsert ignore rules: %v", err)
	}

	ref := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, ref.JobID)
	runQueuedProjectDiscoveryJobs(t, ctx, runtime, jobStore, ref.JobID)

	scanJob, err := jobStore.Get(ctx, ref.JobID)
	if err != nil {
		t.Fatalf("get scan job: %v", err)
	}
	var scanResult scanner.GlobalScanResult
	if err := json.Unmarshal(scanJob.ResultPayload, &scanResult); err != nil {
		t.Fatalf("decode scan result: %v", err)
	}
	if scanResult.ProjectsCreated != 2 || scanResult.ProjectDiscoveryJobsEnqueued != 2 {
		t.Fatalf("unexpected postgres global scan result: %+v", scanResult)
	}

	projects, err := scannerStore.ListProjects(ctx, scanner.ProjectListOptions{Status: "all"})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects.Items) != 2 {
		t.Fatalf("projects count = %d, want 2: %+v", len(projects.Items), projects.Items)
	}
	if projects.Items[0].RepositoryID == "" || projects.Items[0].RepositoryID != projects.Items[1].RepositoryID {
		t.Fatalf("projects must share discovered repository: %+v", projects.Items)
	}

	repo, err := scannerStore.GetRepository(ctx, projects.Items[0].RepositoryID)
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	if repo.Provider != scanner.RepositoryProviderGeneric || repo.ProviderHost != scanner.RepositoryHostLocal || repo.FullPath != "repo" || repo.RootPathID != roots[0].ID {
		t.Fatalf("unexpected postgres repository card: %+v", repo)
	}

	links, err := scannerStore.ListProjectLinks(ctx, scanner.ProjectLinkListOptions{ProjectID: projects.Items[0].ID})
	if err != nil {
		t.Fatalf("list project links: %v", err)
	}
	if len(links.Items) != 1 || links.Items[0].LinkType != scanner.LinkTypeSameRepository || links.Items[0].RepositoryID != repo.ID {
		t.Fatalf("unexpected postgres project links: %+v", links.Items)
	}

	if err := os.RemoveAll(filepath.Join(rootDir, "repo", "app2")); err != nil {
		t.Fatalf("remove app2 fixture: %v", err)
	}
	second := enqueueGlobalScan(t, ctx, jobStore, roots[0].ID)
	runUntilComplete(t, ctx, runtime, jobStore, second.JobID)
	missing, err := scannerStore.ListProjects(ctx, scanner.ProjectListOptions{Status: scanner.ProjectStatusMissing})
	if err != nil {
		t.Fatalf("list missing projects: %v", err)
	}
	if len(missing.Items) != 1 || missing.Items[0].RelativePath != "repo/app2" {
		t.Fatalf("unexpected missing projects after postgres rescan: %+v", missing.Items)
	}
}

func runQueuedProjectDiscoveryJobs(t *testing.T, ctx context.Context, runtime *jobs.Runtime, store *jobs.Store, parentJobID string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		children, err := store.List(ctx, jobs.ListFilters{JobType: "project_discovery", ParentJobID: parentJobID})
		if err != nil {
			t.Fatalf("list project discovery jobs: %v", err)
		}
		pending := false
		for _, child := range children {
			switch child.Status {
			case jobs.StatusQueued, jobs.StatusRunning:
				pending = true
			case jobs.StatusFailed, jobs.StatusCancelled:
				t.Fatalf("project discovery job %s finished with %s: %s", child.ID, child.Status, child.ErrorMessage)
			}
		}
		if !pending {
			return
		}
		if _, err := runtime.RunOnce(ctx); err != nil {
			t.Fatalf("runtime run project discovery: %v", err)
		}
	}
	t.Fatal("project discovery jobs did not complete")
}

func requirePostgresScannerTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse THELPER_POSTGRES_DSN: %v", err)
	}
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if strings.HasSuffix(dbName, "_test") || strings.Contains(dbName, "test") {
		return
	}
	if os.Getenv("THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS") == "1" {
		return
	}
	t.Fatalf("refusing destructive scanner contract test against database %q; use a test database or set THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS=1", dbName)
}

func resetScannerPostgresTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		"DROP TABLE IF EXISTS project_links CASCADE",
		"DROP TABLE IF EXISTS workspaces CASCADE",
		"DROP TABLE IF EXISTS projects CASCADE",
		"DROP TABLE IF EXISTS repositories CASCADE",
		"DROP TABLE IF EXISTS environments CASCADE",
		"DROP TABLE IF EXISTS root_paths CASCADE",
		"DROP TABLE IF EXISTS workflow_statuses CASCADE",
		"DROP TABLE IF EXISTS job_events CASCADE",
		"DROP TABLE IF EXISTS job_locks CASCADE",
		"DROP TABLE IF EXISTS jobs CASCADE",
		"DROP TABLE IF EXISTS ignore_rules CASCADE",
		"DROP TABLE IF EXISTS module_states CASCADE",
		"DROP TABLE IF EXISTS storage_provider_settings CASCADE",
		"DROP TABLE IF EXISTS storage_profiles CASCADE",
		"DROP TABLE IF EXISTS config_entries CASCADE",
		"DROP TABLE IF EXISTS system_metadata CASCADE",
		"DROP TABLE IF EXISTS goose_db_version CASCADE",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("reset postgres table with %q: %v", stmt, err)
		}
	}
}
