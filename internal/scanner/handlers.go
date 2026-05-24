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
}

type projectDiscoveryHandler struct {
	store *Store
}

func JobHandlers(store *Store) map[string]jobs.Handler {
	return map[string]jobs.Handler{
		"global_scan":       globalScanHandler{store: store},
		"project_discovery": projectDiscoveryHandler{store: store},
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
		rootErrorsBefore := result.ErrorsCount
		processed, seen := h.scanRoot(ctx, env, job, root, newIgnoreMatcher(rules), now, &result)
		if processed {
			processedRoots++
			if result.ErrorsCount > rootErrorsBefore {
				_ = env.EmitProgress(ctx, job, "missing project marking skipped for partial root scan", map[string]any{
					"root_path_id": root.ID,
					"error_code":   "partial_root_scan",
				})
				continue
			}
			missing, err := h.store.MarkMissingProjects(ctx, root.ID, seen, now)
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

func (h globalScanHandler) scanRoot(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, root RootPath, matcher ignoreMatcher, now time.Time, result *GlobalScanResult) (bool, map[string]bool) {
	seen := map[string]bool{}
	info, err := os.Lstat(root.Path)
	if err != nil {
		result.ErrorsCount++
		_ = emitScanError(ctx, env, job, "root_path_unavailable", root.ID, root.Path, ".", err)
		return false, seen
	}
	if info.Mode()&os.ModeSymlink != 0 {
		result.ErrorsCount++
		result.DirectoriesSkipped++
		result.SymlinksSkipped++
		_ = emitScanError(ctx, env, job, "root_path_symlink_unsupported", root.ID, root.Path, ".", nil)
		return false, seen
	}
	if !info.IsDir() {
		result.ErrorsCount++
		_ = emitScanError(ctx, env, job, "root_path_unavailable", root.ID, root.Path, ".", nil)
		return false, seen
	}
	queue := []string{root.Path}
	processed := false
	workerCount := h.traversalWorkerCount()
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

			children, stop := h.scanDirectory(ctx, env, job, root, dir, matcher, now, result, seen, &processed, &mu)

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
	return processed, seen
}

func (h globalScanHandler) traversalWorkerCount() int {
	if h.store != nil && h.store.handle != nil && h.store.handle.Provider == "sqlite" {
		return 1
	}
	return 4
}

func (h globalScanHandler) scanDirectory(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, root RootPath, dir string, matcher ignoreMatcher, now time.Time, result *GlobalScanResult, seen map[string]bool, processed *bool, mu *sync.Mutex) ([]string, bool) {
	addError := func(code, pathValue, relativePath string, err error) {
		mu.Lock()
		result.ErrorsCount++
		mu.Unlock()
		_ = emitScanError(ctx, env, job, code, root.ID, pathValue, relativePath, err)
	}
	if ctx.Err() != nil {
		addError("scan_cancelled", root.Path, ".", ctx.Err())
		return nil, true
	}
	dirRelativePath := relativePath(root.Path, dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		addError("read_directory_failed", dir, dirRelativePath, err)
		return nil, false
	}
	mu.Lock()
	*processed = true
	mu.Unlock()
	if containsTerraformFile(entries) {
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
		result.DirectoriesSkipped += countChildDirectories(entries, dir)
		mu.Unlock()
		if err := h.enqueueProjectDiscovery(ctx, env, job, project); err != nil {
			addError("project_discovery_enqueue_failed", dir, dirRelativePath, err)
			return nil, false
		}
		mu.Lock()
		result.ProjectDiscoveryJobsEnqueued++
		mu.Unlock()
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
			if isDirectorySymlink(childPath) {
				mu.Lock()
				result.DirectoriesSkipped++
				result.SymlinksSkipped++
				mu.Unlock()
			}
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

func (h globalScanHandler) enqueueProjectDiscovery(ctx context.Context, env jobs.HandlerEnv, job jobs.Job, project Project) error {
	payload, err := marshalJSON(ProjectDiscoveryPayload{
		SchemaVersion: ProjectDiscoveryPayloadSchema,
		ProjectID:     project.ID,
		RootPathID:    project.RootPathID,
		RelativePath:  project.RelativePath,
		Reason:        "global_scan",
	})
	if err != nil {
		return err
	}
	ref, err := env.Store.Enqueue(ctx, jobs.EnqueueRequest{
		JobType:     "project_discovery",
		Actor:       nonEmpty(job.Actor, "global-scanner"),
		ParentJobID: job.ID,
		LockKey:     "project_discovery:" + project.ID,
		Payload:     payload,
	})
	if err != nil {
		return err
	}
	return env.EmitChildCreated(ctx, job, "project discovery job enqueued", map[string]any{
		"project_id": project.ID,
		"job_id":     ref.JobID,
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
	repositoryPath, markerType, detected, err := findGitRepository(project.Path, root.Path)
	if err != nil {
		return nil, jobs.HandlerError{Code: "handler_failed", Message: err.Error(), Retryable: true}
	}
	result := ProjectDiscoveryResult{
		SchemaVersion:    ProjectDiscoveryResultSchema,
		ProjectID:        project.ID,
		LinkedProjectIDs: []string{},
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

func findGitRepository(projectPath, rootPath string) (string, string, bool, error) {
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
		info, err := os.Lstat(markerPath)
		if err == nil {
			if info.IsDir() {
				return current, ".git_directory", true, nil
			}
			if info.Mode().IsRegular() {
				valid, err := validGitdirFile(markerPath)
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

func validGitdirFile(path string) (bool, error) {
	file, err := os.Open(path)
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

func containsTerraformFile(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".tf") {
			return true
		}
	}
	return false
}

func countChildDirectories(entries []os.DirEntry, parent string) int {
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 && isDirectorySymlink(filepath.Join(parent, entry.Name())) {
			count++
		}
	}
	return count
}

func isDirectorySymlink(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
