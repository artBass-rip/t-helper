package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
)

type globalScanHandler struct {
	store *Store
	fs    scanFilesystem
}

type projectDiscoveryHandler struct {
	store *Store
	fs    scanFilesystem
}

type scanFilesystem interface {
	Lstat(name string) (os.FileInfo, error)
	ReadDir(name string) ([]os.DirEntry, error)
	Open(name string) (io.ReadCloser, error)
}

type osScanFilesystem struct{}

func (osScanFilesystem) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

func (osScanFilesystem) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

func (osScanFilesystem) Open(name string) (io.ReadCloser, error) {
	return os.Open(name)
}

func JobHandlers(store *Store) map[string]jobs.Handler {
	fs := osScanFilesystem{}
	return map[string]jobs.Handler{
		"global_scan":       globalScanHandler{store: store, fs: fs},
		"project_discovery": projectDiscoveryHandler{store: store, fs: fs},
	}
}

func (h globalScanHandler) Handle(ctx context.Context, env jobs.HandlerEnv, job jobs.Job) (json.RawMessage, error) {
	var payload GlobalScanPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if payload.SchemaVersion != GlobalScanPayloadSchema {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "invalid global scan payload schema_version", Retryable: false}
	}
	if payload.FollowSymlinks {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "follow_symlinks=true is not supported", Retryable: false}
	}
	if err := h.store.SyncRootPathsFromConfig(ctx); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	roots, err := h.roots(ctx, payload.RootPathIDs)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if len(roots) == 0 {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "no enabled root paths to scan", Retryable: false}
	}
	result := GlobalScanResult{SchemaVersion: GlobalScanResultSchema, RootPathIDs: make([]string, 0, len(roots))}
	processedRoots := 0
	now := time.Now().UTC()
	for _, root := range roots {
		result.RootPathIDs = append(result.RootPathIDs, root.ID)
		rules, err := h.store.IgnoreRulesForRoot(ctx, root.ID)
		if err != nil {
			return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
		}
		before := result
		startedAt := time.Now()
		processed, seen, erroredSubtrees := h.scanRoot(ctx, env, job, root, newIgnoreMatcher(rules), now, &result)
		_ = env.EmitProgress(ctx, job, "root scan completed", rootScanMetrics(root.ID, time.Since(startedAt), h.traversalWorkerCount(ctx), before, result))
		if processed {
			processedRoots++
			if len(erroredSubtrees) > 0 {
				_ = env.EmitProgress(ctx, job, "missing project marking skipped for errored subtrees", map[string]any{
					"root_path_id":          root.ID,
					"errored_subtree_count": len(erroredSubtrees),
				})
			}
			missing, err := h.store.MarkMissingProjects(ctx, root.ID, seen, erroredSubtrees, now)
			if err != nil {
				return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
			}
			result.ProjectsMarkedMissing += missing
		}
	}
	if processedRoots == 0 {
		return nil, jobs.HandlerError{Code: "global_scan_failed", Message: "all requested root paths failed before traversal", Retryable: false}
	}
	return marshalJSON(result)
}

func (h globalScanHandler) roots(ctx context.Context, ids []string) ([]RootPath, error) {
	if len(ids) > 0 {
		return h.store.RootPathsByIDs(ctx, ids)
	}
	return h.store.EnabledRootPaths(ctx)
}

func (h globalScanHandler) filesystem() scanFilesystem {
	if h.fs != nil {
		return h.fs
	}
	return osScanFilesystem{}
}

func (h projectDiscoveryHandler) filesystem() scanFilesystem {
	if h.fs != nil {
		return h.fs
	}
	return osScanFilesystem{}
}

func rootScanMetrics(rootPathID string, duration time.Duration, workers int, before, after GlobalScanResult) map[string]any {
	return map[string]any{
		"root_path_id":                    rootPathID,
		"duration_ms":                     duration.Milliseconds(),
		"traversal_workers":               workers,
		"projects_created":                after.ProjectsCreated - before.ProjectsCreated,
		"projects_updated":                after.ProjectsUpdated - before.ProjectsUpdated,
		"project_discovery_jobs_enqueued": after.ProjectDiscoveryJobsEnqueued - before.ProjectDiscoveryJobsEnqueued,
		"directories_skipped":             after.DirectoriesSkipped - before.DirectoriesSkipped,
		"symlinks_skipped":                after.SymlinksSkipped - before.SymlinksSkipped,
		"errors_count":                    after.ErrorsCount - before.ErrorsCount,
	}
}

func (h globalScanHandler) scanRoot(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, root RootPath, matcher ignoreMatcher, now time.Time, result *GlobalScanResult) (bool, map[string]bool, map[string]bool) {
	seen := map[string]bool{}
	erroredSubtrees := map[string]bool{}
	info, err := h.filesystem().Lstat(root.Path)
	if err != nil {
		result.ErrorsCount++
		_ = emitScanError(ctx, env, job, "root_path_unavailable", root.ID, root.Path, ".", err)
		return false, seen, erroredSubtrees
	}
	if info.Mode()&os.ModeSymlink != 0 {
		result.ErrorsCount++
		result.DirectoriesSkipped++
		result.SymlinksSkipped++
		_ = emitScanError(ctx, env, job, "root_path_symlink_unsupported", root.ID, root.Path, ".", nil)
		return false, seen, erroredSubtrees
	}
	if !info.IsDir() {
		result.ErrorsCount++
		_ = emitScanError(ctx, env, job, "root_path_unavailable", root.ID, root.Path, ".", nil)
		return false, seen, erroredSubtrees
	}
	queue := []string{root.Path}
	processed := false
	workerCount := h.traversalWorkerCount(ctx)
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	active := 0
	worker := func() {
		for {
			mu.Lock()
			for len(queue) == 0 && active > 0 {
				cond.Wait()
			}
			if len(queue) == 0 && active == 0 {
				mu.Unlock()
				return
			}
			dir := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			active++
			mu.Unlock()

			children, stop := h.scanDirectory(ctx, env, job, root, dir, matcher, now, result, seen, erroredSubtrees, &processed, &mu)

			mu.Lock()
			if !stop {
				queue = append(queue, children...)
			}
			active--
			cond.Broadcast()
			mu.Unlock()
		}
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			worker()
		}()
	}
	workers.Wait()
	return processed, seen, erroredSubtrees
}

func (h globalScanHandler) traversalWorkerCount(ctx context.Context) int {
	if h.store != nil && h.store.handle != nil && h.store.handle.Provider == "sqlite" {
		return 1
	}
	if h.store != nil {
		if configured, err := h.store.ConfigInt(ctx, "workers.concurrency"); err == nil && configured > 0 {
			if configured > 64 {
				return 64
			}
			return configured
		}
	}
	return 4
}

func (h globalScanHandler) scanDirectory(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, root RootPath, dir string, matcher ignoreMatcher, now time.Time, result *GlobalScanResult, seen map[string]bool, erroredSubtrees map[string]bool, processed *bool, mu *sync.Mutex) ([]string, bool) {
	addError := func(code, pathValue, relativePath string, err error) {
		mu.Lock()
		result.ErrorsCount++
		erroredSubtrees[cleanRelativePath(relativePath)] = true
		mu.Unlock()
		_ = emitScanError(ctx, env, job, code, root.ID, pathValue, relativePath, err)
	}
	if ctx.Err() != nil {
		addError("scan_cancelled", root.Path, ".", ctx.Err())
		return nil, true
	}
	dirRelativePath := relativePath(root.Path, dir)
	entries, err := h.filesystem().ReadDir(dir)
	if err != nil {
		addError("read_directory_failed", dir, dirRelativePath, err)
		return nil, false
	}
	mu.Lock()
	*processed = true
	mu.Unlock()
	if containsTerraformFile(entries, dirRelativePath, matcher) {
		project, created, err := h.store.UpsertProject(ctx, root, dirRelativePath, now)
		if err != nil {
			addError("project_upsert_failed", dir, dirRelativePath, err)
			return nil, false
		}
		mu.Lock()
		seen[project.RelativePath] = true
		if created {
			result.ProjectsCreated++
		} else {
			result.ProjectsUpdated++
		}
		childDirectories, childSymlinks := countSkippedChildren(entries)
		result.DirectoriesSkipped += childDirectories
		result.SymlinksSkipped += childSymlinks
		mu.Unlock()
		enqueued, err := h.enqueueProjectDiscovery(ctx, env, job, project)
		if err != nil {
			addError("project_discovery_enqueue_failed", dir, dirRelativePath, err)
			return nil, false
		}
		if enqueued {
			mu.Lock()
			result.ProjectDiscoveryJobsEnqueued++
			mu.Unlock()
		}
		return nil, false
	}
	children := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		childPath := filepath.Join(dir, entry.Name())
		childRelativePath := relativePath(root.Path, childPath)
		if entry.Name() == ".git" {
			mu.Lock()
			result.DirectoriesSkipped++
			if entry.Type()&os.ModeSymlink != 0 {
				result.SymlinksSkipped++
			}
			mu.Unlock()
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			mu.Lock()
			result.DirectoriesSkipped++
			result.SymlinksSkipped++
			mu.Unlock()
			continue
		}
		if matcher.ignored(childRelativePath, true) {
			mu.Lock()
			result.DirectoriesSkipped++
			mu.Unlock()
			continue
		}
		children = append(children, childPath)
	}
	return children, false
}

func (h globalScanHandler) enqueueProjectDiscovery(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, project Project) (bool, error) {
	payload, err := marshalJSON(ProjectDiscoveryPayload{
		SchemaVersion: ProjectDiscoveryPayloadSchema,
		ProjectID:     project.ID,
		RootPathID:    project.RootPathID,
		RelativePath:  project.RelativePath,
		Reason:        "global_scan",
	})
	if err != nil {
		return false, err
	}
	lockKey := "project_discovery:" + project.ID
	ref, enqueued, err := env.Store.EnqueueIfNoActive(ctx, jobs.EnqueueRequest{
		JobType:     "project_discovery",
		Actor:       nonEmpty(job.Actor, "global-scanner"),
		ParentJobID: job.ID,
		LockKey:     lockKey,
		Payload:     payload,
	})
	if err != nil {
		return false, err
	}
	message := "project discovery job enqueued"
	if !enqueued {
		message = "project discovery job already active"
	}
	return enqueued, env.EmitChildCreated(ctx, job, message, map[string]any{
		"project_id": project.ID,
		"job_id":     ref.JobID,
		"coalesced":  !enqueued,
	})
}

func (h projectDiscoveryHandler) Handle(ctx context.Context, env jobs.HandlerEnv, job jobs.Job) (json.RawMessage, error) {
	var payload ProjectDiscoveryPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	if payload.SchemaVersion != ProjectDiscoveryPayloadSchema {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "invalid project discovery payload schema_version", Retryable: false}
	}
	project, err := h.store.GetProject(ctx, payload.ProjectID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	result := ProjectDiscoveryResult{
		SchemaVersion:    ProjectDiscoveryResultSchema,
		ProjectID:        project.ID,
		LinkedProjectIDs: []string{},
	}
	if project.Status == ProjectStatusMissing || project.Status == ProjectStatusDisabled {
		_ = env.EmitProgress(ctx, job, "project discovery skipped for inactive project", map[string]any{
			"project_id": project.ID,
			"status":     project.Status,
		})
		return marshalJSON(result)
	}
	if payload.RootPathID != "" && payload.RootPathID != project.RootPathID {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "project discovery root_path_id does not match project", Retryable: false}
	}
	if payload.RelativePath != "" && cleanRelativePath(payload.RelativePath) != project.RelativePath {
		return nil, jobs.HandlerError{Code: "validation_error", Message: "project discovery relative_path does not match project", Retryable: false}
	}
	root, err := h.store.GetRootPath(ctx, project.RootPathID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "validation_error", Message: err.Error(), Retryable: false}
	}
	repositoryPath, markerType, detected, err := findGitRepository(h.filesystem(), project.Path, root.Path)
	if err != nil {
		return nil, jobs.HandlerError{Code: "handler_failed", Message: err.Error(), Retryable: true}
	}
	if !detected {
		return marshalJSON(result)
	}
	repo, created, updated, err := h.store.UpsertGenericRepository(ctx, root, repositoryPath)
	if err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	if err := h.store.SetProjectRepository(ctx, project.ID, repo.ID); err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	related, err := h.store.ProjectsByRepository(ctx, repo.ID)
	if err != nil {
		return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
	}
	for _, other := range related {
		if other.ID == project.ID {
			continue
		}
		createdLink, err := h.store.UpsertProjectLink(ctx, project.ID, other.ID, repo.ID, job.ID)
		if err != nil {
			return nil, jobs.HandlerError{Code: "storage_error", Message: err.Error(), Retryable: true}
		}
		result.LinkedProjectIDs = append(result.LinkedProjectIDs, other.ID)
		if createdLink {
			result.LinksCreated++
		}
	}
	result.GitRepositoryDetected = true
	result.RepositoryID = repo.ID
	result.RepositoryCreated = created
	result.RepositoryUpdated = updated
	result.GitMarkerType = markerType
	_ = env.EmitProgress(ctx, job, "project discovery completed", map[string]any{
		"project_id":      project.ID,
		"repository_id":   repo.ID,
		"git_marker_type": markerType,
	})
	return marshalJSON(result)
}

func findGitRepository(fs scanFilesystem, projectPath, rootPath string) (string, string, bool, error) {
	projectPath, err := normalizeAbsPath(projectPath)
	if err != nil {
		return "", "", false, err
	}
	rootPath, err = normalizeAbsPath(rootPath)
	if err != nil {
		return "", "", false, err
	}
	current := projectPath
	for {
		if !withinRoot(rootPath, current) {
			return "", "", false, nil
		}
		markerPath := filepath.Join(current, ".git")
		info, err := fs.Lstat(markerPath)
		if err == nil {
			if info.IsDir() {
				return current, ".git_directory", true, nil
			}
			if info.Mode().IsRegular() {
				valid, err := validGitdirFile(fs, markerPath)
				if err != nil {
					return "", "", false, err
				}
				if valid {
					return current, ".git_file", true, nil
				}
			}
		} else if !os.IsNotExist(err) {
			return "", "", false, err
		}
		if current == rootPath {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", "", false, nil
}

func validGitdirFile(fs scanFilesystem, path string) (bool, error) {
	file, err := fs.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4*1024))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "gitdir:"), nil
	}
	return false, nil
}

func containsTerraformFile(entries []os.DirEntry, dirRelativePath string, matcher ignoreMatcher) bool {
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".tf") {
			fileRelativePath := cleanRelativePath(filepath.ToSlash(filepath.Join(dirRelativePath, entry.Name())))
			if matcher.ignored(fileRelativePath, false) {
				continue
			}
			return true
		}
	}
	return false
}

func countSkippedChildren(entries []os.DirEntry) (int, int) {
	directories := 0
	symlinks := 0
	for _, entry := range entries {
		if entry.IsDir() {
			directories++
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			directories++
			symlinks++
		}
	}
	return directories, symlinks
}

func relativePath(rootPath, childPath string) string {
	rel, err := filepath.Rel(rootPath, childPath)
	if err != nil {
		return "."
	}
	return cleanRelativePath(filepath.ToSlash(rel))
}

func withinRoot(rootPath, childPath string) bool {
	rel, err := filepath.Rel(rootPath, childPath)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func emitScanError(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, code, rootPathID, pathValue, relativePath string, err error) error {
	message := code
	if err != nil {
		message = fmt.Sprintf("%s: %s", code, err.Error())
	}
	return env.EmitProgress(ctx, job, message, map[string]any{
		"error_code":    code,
		"root_path_id":  rootPathID,
		"path":          pathValue,
		"relative_path": relativePath,
	})
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
