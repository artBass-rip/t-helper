package scanner

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestGlobalScanFilesystemReadBoundary(t *testing.T) {
	ctx := context.Background()
	handle := openBoundarySQLite(t)
	defer handle.Close()
	scannerStore := NewStore(handle)
	jobStore := jobs.NewStore(handle)
	fs := newBoundaryFilesystem()
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store: jobStore,
		Handlers: map[string]jobs.Handler{
			"global_scan":       globalScanHandler{store: scannerStore, fs: fs},
			"project_discovery": projectDiscoveryHandler{store: scannerStore, fs: fs},
		},
		WorkerID: "host:test:fs-boundary-global",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	mustBoundaryWriteFile(t, filepath.Join(rootDir, "repo", ".git", "config"), "[remote]\n")
	mustBoundaryWriteFile(t, filepath.Join(rootDir, "repo", "service", "main.tf"), "terraform {}\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}

	ref := enqueueBoundaryGlobalScan(t, ctx, jobStore, roots[0].ID)
	runBoundaryUntilComplete(t, ctx, runtime, jobStore, ref.JobID)

	if opened := fs.openedWithSuffix(".tf"); len(opened) != 0 {
		t.Fatalf("global scan opened Terraform source files: %v", opened)
	}
	if opened := fs.openedWithSuffix(filepath.Join(".git", "config")); len(opened) != 0 {
		t.Fatalf("global scan opened .git/config: %v", opened)
	}
	if opened := fs.openedWithBase(".git"); len(opened) != 0 {
		t.Fatalf("global scan opened .git marker files: %v", opened)
	}
}

func TestProjectDiscoveryFilesystemReadBoundary(t *testing.T) {
	ctx := context.Background()
	handle := openBoundarySQLite(t)
	defer handle.Close()
	scannerStore := NewStore(handle)
	jobStore := jobs.NewStore(handle)
	fs := newBoundaryFilesystem()
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store: jobStore,
		Handlers: map[string]jobs.Handler{
			"project_discovery": projectDiscoveryHandler{store: scannerStore, fs: fs},
		},
		WorkerID: "host:test:fs-boundary-discovery",
		Logger:   slog.Default(),
	})

	rootDir := t.TempDir()
	gitFilePayload := "\n  gitdir: ../.git/worktrees/service\n" + strings.Repeat("x", 8192)
	mustBoundaryWriteFile(t, filepath.Join(rootDir, "service", ".git"), gitFilePayload)
	mustBoundaryWriteFile(t, filepath.Join(rootDir, "service", ".gitconfig"), "not a marker\n")
	mustBoundaryWriteFile(t, filepath.Join(rootDir, "service", ".gitmodules"), "not a marker\n")
	mustBoundaryWriteFile(t, filepath.Join(rootDir, "service", "main.tf"), "terraform {}\n")
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	project, _, err := scannerStore.UpsertProject(ctx, roots[0], "service", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}

	ref := enqueueBoundaryProjectDiscovery(t, ctx, jobStore, project)
	runBoundaryUntilComplete(t, ctx, runtime, jobStore, ref.JobID)

	openedGit := fs.openedWithBase(".git")
	if len(openedGit) != 1 || filepath.Clean(openedGit[0]) != filepath.Join(rootDir, "service", ".git") {
		t.Fatalf("project discovery should open only .git marker file, got %v", openedGit)
	}
	if opened := fs.openedWithSuffix(filepath.Join(".git", "config")); len(opened) != 0 {
		t.Fatalf("project discovery opened .git/config: %v", opened)
	}
	if opened := fs.openedWithBase(".gitmodules"); len(opened) != 0 {
		t.Fatalf("project discovery opened negative git-like marker: %v", opened)
	}
	if read := fs.bytesRead(filepath.Join(rootDir, "service", ".git")); read > 4096 {
		t.Fatalf("project discovery read %d bytes from .git marker file, want <= 4096", read)
	}
}

func TestRootPathsByIDsDeduplicatesRequestedRoots(t *testing.T) {
	ctx := context.Background()
	handle := openBoundarySQLite(t)
	defer handle.Close()
	scannerStore := NewStore(handle)

	rootDir := t.TempDir()
	enabled := true
	roots, err := scannerStore.UpsertRootPaths(ctx, []RootPathInput{{Name: "root", Path: rootDir, Enabled: &enabled}})
	if err != nil {
		t.Fatalf("upsert root path: %v", err)
	}
	resolved, err := scannerStore.RootPathsByIDs(ctx, []string{roots[0].ID, " ", roots[0].ID})
	if err != nil {
		t.Fatalf("resolve root paths: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != roots[0].ID {
		t.Fatalf("resolved roots = %+v, want single deduplicated root", resolved)
	}
}

type boundaryFilesystem struct {
	mu       sync.Mutex
	opens    []string
	reads    map[string]int
	delegate osScanFilesystem
}

func newBoundaryFilesystem() *boundaryFilesystem {
	return &boundaryFilesystem{reads: map[string]int{}}
}

func (fs *boundaryFilesystem) Lstat(name string) (os.FileInfo, error) {
	return fs.delegate.Lstat(name)
}

func (fs *boundaryFilesystem) ReadDir(name string) ([]os.DirEntry, error) {
	return fs.delegate.ReadDir(name)
}

func (fs *boundaryFilesystem) Open(name string) (io.ReadCloser, error) {
	reader, err := fs.delegate.Open(name)
	if err != nil {
		return nil, err
	}
	clean := filepath.Clean(name)
	fs.mu.Lock()
	fs.opens = append(fs.opens, clean)
	fs.mu.Unlock()
	return &countingReadCloser{ReadCloser: reader, path: clean, fs: fs}, nil
}

func (fs *boundaryFilesystem) openedWithSuffix(suffix string) []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []string
	for _, path := range fs.opens {
		if strings.HasSuffix(path, suffix) {
			out = append(out, path)
		}
	}
	return out
}

func (fs *boundaryFilesystem) openedWithBase(base string) []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []string
	for _, path := range fs.opens {
		if filepath.Base(path) == base {
			out = append(out, path)
		}
	}
	return out
}

func (fs *boundaryFilesystem) bytesRead(path string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.reads[filepath.Clean(path)]
}

type countingReadCloser struct {
	io.ReadCloser
	path string
	fs   *boundaryFilesystem
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.fs.mu.Lock()
	r.fs.reads[r.path] += n
	r.fs.mu.Unlock()
	return n, err
}

func enqueueBoundaryGlobalScan(t *testing.T, ctx context.Context, store *jobs.Store, rootPathID string) jobs.JobRef {
	t.Helper()
	payload, err := json.Marshal(GlobalScanPayload{
		SchemaVersion:  GlobalScanPayloadSchema,
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

func enqueueBoundaryProjectDiscovery(t *testing.T, ctx context.Context, store *jobs.Store, project Project) jobs.JobRef {
	t.Helper()
	payload, err := json.Marshal(ProjectDiscoveryPayload{
		SchemaVersion: ProjectDiscoveryPayloadSchema,
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

func runBoundaryUntilComplete(t *testing.T, ctx context.Context, runtime *jobs.Runtime, store *jobs.Store, jobID string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		job, err := store.Get(ctx, jobID)
		if err != nil {
			t.Fatalf("get job %s: %v", jobID, err)
		}
		if job.Status == jobs.StatusSucceeded {
			return
		}
		if job.Status == jobs.StatusFailed || job.Status == jobs.StatusCancelled {
			t.Fatalf("job %s finished with %s: %s", jobID, job.Status, job.ErrorMessage)
		}
		if _, err := runtime.RunOnce(ctx); err != nil {
			t.Fatalf("runtime run once: %v", err)
		}
	}
	t.Fatalf("job %s did not complete", jobID)
}

func mustBoundaryWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func openBoundarySQLite(t *testing.T) *storage.Handle {
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
