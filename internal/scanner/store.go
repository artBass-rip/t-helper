package scanner

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/storage"
)

var (
	ErrNotFound      = errors.New("scanner registry record not found")
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrValidation    = errors.New("scanner validation error")
)

type Store struct {
	handle *storage.Handle
}

func NewStore(handle *storage.Handle) *Store {
	return &Store{handle: handle}
}

func (s *Store) SyncRootPathsFromConfig(ctx context.Context) error {
	query := "SELECT value FROM config_entries WHERE scope = ? AND key = ?"
	args := []any{"system", "scanning.global_scan"}
	if s.handle.Provider == "postgres" {
		query = "SELECT value FROM config_entries WHERE scope = $1 AND key = $2"
	}
	var raw string
	if err := s.handle.DB.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	var roots []struct {
		Name      string `json:"name"`
		RootPath  string `json:"root_path"`
		Schedule  bool   `json:"schedule"`
		Frequency string `json:"frequency"`
	}
	if err := json.Unmarshal([]byte(raw), &roots); err != nil {
		return fmt.Errorf("decode scanning.global_scan: %w", err)
	}
	inputs := make([]RootPathInput, 0, len(roots))
	currentPaths := make(map[string]bool, len(roots))
	for _, root := range roots {
		enabled := true
		schedule := root.Schedule
		normalized, err := normalizeAbsPath(root.RootPath)
		if err != nil {
			return err
		}
		currentPaths[normalized] = true
		inputs = append(inputs, RootPathInput{
			Name:              root.Name,
			Path:              normalized,
			Enabled:           &enabled,
			ScheduleEnabled:   &schedule,
			ScheduleFrequency: root.Frequency,
			Source:            RootPathSourceConfig,
		})
	}
	if _, err := s.UpsertRootPaths(ctx, inputs); err != nil {
		return err
	}
	return s.deactivateRemovedConfigRootPaths(ctx, currentPaths)
}

func (s *Store) UpsertRootPaths(ctx context.Context, inputs []RootPathInput) ([]RootPath, error) {
	out := make([]RootPath, 0, len(inputs))
	for _, input := range inputs {
		item, err := s.upsertRootPath(ctx, input)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) upsertRootPath(ctx context.Context, input RootPathInput) (RootPath, error) {
	normalized, err := normalizeAbsPath(input.Path)
	if err != nil {
		return RootPath{}, err
	}
	frequency := strings.TrimSpace(input.ScheduleFrequency)
	if err := validateFrequency(frequency); err != nil {
		return RootPath{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(normalized)
	}
	source := strings.TrimSpace(input.Source)
	sourceExplicit := source != ""
	if source != RootPathSourceAPI && source != RootPathSourceConfig {
		if sourceExplicit {
			return RootPath{}, validationErrorf("unsupported root_path source %q", source)
		}
	}
	now := time.Now().UTC()
	existing, err := s.findRootPath(ctx, input.ID, normalized)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return RootPath{}, err
	}
	if err == nil {
		if !sourceExplicit {
			source = existing.Source
		}
		enabled := existing.Enabled
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		schedule := existing.ScheduleEnabled
		if input.ScheduleEnabled != nil {
			schedule = *input.ScheduleEnabled
		}
		query := `UPDATE root_paths SET name = ?, path = ?, source = ?, enabled = ?, schedule_enabled = ?, schedule_frequency = ?, updated_at = ? WHERE id = ?`
		args := []any{name, normalized, source, s.boolArg(enabled), s.boolArg(schedule), nullEmpty(frequency), formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE root_paths SET name = $1, path = $2, source = $3, enabled = $4, schedule_enabled = $5, schedule_frequency = $6, updated_at = $7 WHERE id = $8`
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			return RootPath{}, err
		}
		return s.GetRootPath(ctx, existing.ID)
	}
	enabled := true
	if !sourceExplicit {
		source = RootPathSourceAPI
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	schedule := false
	if input.ScheduleEnabled != nil {
		schedule = *input.ScheduleEnabled
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = newID("root")
	}
	query := `INSERT INTO root_paths (id, name, path, source, enabled, schedule_enabled, schedule_frequency, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{id, name, normalized, source, s.boolArg(enabled), s.boolArg(schedule), nullEmpty(frequency), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO root_paths (id, name, path, source, enabled, schedule_enabled, schedule_frequency, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			existing, findErr := s.findRootPath(ctx, "", normalized)
			if findErr != nil {
				return RootPath{}, err
			}
			input.ID = existing.ID
			return s.upsertRootPath(ctx, input)
		}
		return RootPath{}, err
	}
	return s.GetRootPath(ctx, id)
}

func (s *Store) deactivateRemovedConfigRootPaths(ctx context.Context, currentPaths map[string]bool) error {
	now := time.Now().UTC()
	query := "SELECT id, path FROM root_paths WHERE source = ? AND enabled = ?"
	args := []any{RootPathSourceConfig, s.boolArg(true)}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, path FROM root_paths WHERE source = $1 AND enabled = $2"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var removed []string
	for rows.Next() {
		var id, pathValue string
		if err := rows.Scan(&id, &pathValue); err != nil {
			rows.Close()
			return err
		}
		if !currentPaths[pathValue] {
			removed = append(removed, id)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, batch := range batches(removed, 500) {
		if err := s.updateRootPathsEnabled(ctx, batch, false, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) updateRootPathsEnabled(ctx context.Context, ids []string, enabled bool, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{s.boolArg(enabled), formatTime(now)}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, s.placeholder(len(args)))
	}
	query := fmt.Sprintf("UPDATE root_paths SET enabled = %s, updated_at = %s WHERE id IN (%s)",
		s.placeholder(1), s.placeholder(2), strings.Join(placeholders, ", "))
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) findRootPath(ctx context.Context, id, path string) (RootPath, error) {
	if strings.TrimSpace(id) != "" {
		root, err := s.GetRootPath(ctx, id)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return root, err
		}
	}
	query := "SELECT " + s.rootPathColumns() + " FROM root_paths WHERE path = ?"
	args := []any{path}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.rootPathColumns() + " FROM root_paths WHERE path = $1"
	}
	root, err := scanRootPath(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return RootPath{}, ErrNotFound
	}
	return root, err
}

func (s *Store) GetRootPath(ctx context.Context, id string) (RootPath, error) {
	query := "SELECT " + s.rootPathColumns() + " FROM root_paths WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.rootPathColumns() + " FROM root_paths WHERE id = $1"
	}
	root, err := scanRootPath(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return RootPath{}, ErrNotFound
	}
	return root, err
}

func (s *Store) RootPathByPath(ctx context.Context, path string) (RootPath, error) {
	normalized, err := normalizeAbsPath(path)
	if err != nil {
		return RootPath{}, err
	}
	return s.findRootPath(ctx, "", normalized)
}

func (s *Store) ListRootPaths(ctx context.Context, opts ListOptions) (Page[RootPath], error) {
	query := "SELECT " + s.rootPathColumns() + " FROM root_paths"
	return listPage(ctx, s, query, nil, "created_at", opts, scanRootPath)
}

func (s *Store) EnabledRootPaths(ctx context.Context) ([]RootPath, error) {
	query := "SELECT " + s.rootPathColumns() + " FROM root_paths WHERE enabled = ? ORDER BY path ASC"
	args := []any{s.boolArg(true)}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.rootPathColumns() + " FROM root_paths WHERE enabled = $1 ORDER BY path ASC"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RootPath
	for rows.Next() {
		item, err := scanRootPath(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) RootPathsByIDs(ctx context.Context, ids []string) ([]RootPath, error) {
	out := make([]RootPath, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		root, err := s.GetRootPath(ctx, id)
		if err != nil {
			return nil, err
		}
		if !root.Enabled {
			return nil, fmt.Errorf("root_path %s is disabled", id)
		}
		out = append(out, root)
	}
	return out, nil
}

func (s *Store) ConfigInt(ctx context.Context, key string) (int, error) {
	query := "SELECT value FROM config_entries WHERE scope = ? AND key = ?"
	args := []any{"system", key}
	if s.handle.Provider == "postgres" {
		query = "SELECT value FROM config_entries WHERE scope = $1 AND key = $2"
	}
	var raw string
	if err := s.handle.DB.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, validationErrorf("config %s must be an integer", key)
	}
	return value, nil
}

func (s *Store) UpsertProject(ctx context.Context, root RootPath, relativePath string, now time.Time) (Project, bool, error) {
	relativePath, err := safeRelativePath(relativePath)
	if err != nil {
		return Project{}, false, err
	}
	projectPath := root.Path
	if relativePath != "." {
		projectPath = filepath.Join(root.Path, filepath.FromSlash(relativePath))
	}
	name := filepath.Base(projectPath)
	if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
		name = filepath.Base(root.Path)
	}
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, false, err
	}
	defer tx.Rollback()
	existing, err := s.getProjectByIdentity(ctx, tx, root.ID, relativePath)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Project{}, false, err
	}
	if err == nil {
		query := `UPDATE projects SET name = ?, path = ?, terraform_marker = ?, status = CASE WHEN status = 'disabled' THEN status ELSE ? END, last_seen_at = ?, updated_at = ? WHERE id = ?`
		args := []any{name, projectPath, TerraformMarkerGlob, ProjectStatusActive, formatTime(now), formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE projects SET name = $1, path = $2, terraform_marker = $3, status = CASE WHEN status = 'disabled' THEN status ELSE $4 END, last_seen_at = $5, updated_at = $6 WHERE id = $7`
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return Project{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Project{}, false, err
		}
		project, err := s.GetProject(ctx, existing.ID)
		return project, false, err
	}
	id := newID("project")
	query := `INSERT INTO projects (id, name, path, relative_path, root_path_id, terraform_marker, status, detected_at, last_seen_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{id, name, projectPath, relativePath, root.ID, TerraformMarkerGlob, ProjectStatusActive, formatTime(now), formatTime(now), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO projects (id, name, path, relative_path, root_path_id, terraform_marker, status, detected_at, last_seen_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			_ = tx.Rollback()
			return s.UpsertProject(ctx, root, relativePath, now)
		}
		return Project{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, false, err
	}
	project, err := s.GetProject(ctx, id)
	return project, true, err
}

func (s *Store) getProjectByIdentity(ctx context.Context, tx *sql.Tx, rootPathID, relativePath string) (Project, error) {
	query := "SELECT " + s.projectColumns() + " FROM projects WHERE root_path_id = ? AND relative_path = ?"
	args := []any{rootPathID, relativePath}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.projectColumns() + " FROM projects WHERE root_path_id = $1 AND relative_path = $2"
	}
	project, err := scanProject(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
}

func (s *Store) GetProject(ctx context.Context, id string) (Project, error) {
	query := "SELECT " + s.projectColumns() + " FROM projects WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.projectColumns() + " FROM projects WHERE id = $1"
	}
	project, err := scanProject(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
}

func (s *Store) ListProjects(ctx context.Context, opts ProjectListOptions) (Page[Project], error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.RootPathID != "" {
		add("root_path_id = %s", opts.RootPathID)
	}
	if opts.RepositoryID != "" {
		add("repository_id = %s", opts.RepositoryID)
	}
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = ProjectStatusActive
	}
	if status != "all" {
		if !validProjectStatus(status) {
			return Page[Project]{}, validationErrorf("unsupported project status %q", status)
		}
		add("status = %s", status)
	}
	query := "SELECT " + s.projectColumns() + " FROM projects"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanProject)
}

func (s *Store) MarkMissingProjects(ctx context.Context, rootPathID string, seen map[string]bool, erroredSubtrees map[string]bool, now time.Time) (int, error) {
	query := "SELECT id, relative_path FROM projects WHERE root_path_id = ? AND status = ?"
	args := []any{rootPathID, ProjectStatusActive}
	if s.handle.Provider == "postgres" {
		query = "SELECT id, relative_path FROM projects WHERE root_path_id = $1 AND status = $2"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	var missing []string
	for rows.Next() {
		var id, relativePath string
		if err := rows.Scan(&id, &relativePath); err != nil {
			rows.Close()
			return 0, err
		}
		if !seen[relativePath] && !underErroredSubtree(relativePath, erroredSubtrees) {
			missing = append(missing, id)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, batch := range batches(missing, 500) {
		if err := s.markProjectIDsMissing(ctx, tx, batch, now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(missing), nil
}

func (s *Store) markProjectIDsMissing(ctx context.Context, tx *sql.Tx, ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{ProjectStatusMissing, formatTime(now)}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, s.placeholder(len(args)))
	}
	query := fmt.Sprintf("UPDATE projects SET status = %s, updated_at = %s WHERE id IN (%s)",
		s.placeholder(1), s.placeholder(2), strings.Join(placeholders, ", "))
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) SetProjectRepository(ctx context.Context, projectID, repositoryID string) error {
	query := "UPDATE projects SET repository_id = ?, updated_at = ? WHERE id = ?"
	args := []any{repositoryID, formatTime(time.Now().UTC()), projectID}
	if s.handle.Provider == "postgres" {
		query = "UPDATE projects SET repository_id = $1, updated_at = $2 WHERE id = $3"
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ProjectsByRepository(ctx context.Context, repositoryID string) ([]Project, error) {
	query := "SELECT " + s.projectColumns() + " FROM projects WHERE repository_id = ? ORDER BY id ASC"
	args := []any{repositoryID}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.projectColumns() + " FROM projects WHERE repository_id = $1 ORDER BY id ASC"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertGenericRepository(ctx context.Context, root RootPath, repositoryPath string) (Repository, bool, bool, error) {
	rootPath, err := normalizeAbsPath(root.Path)
	if err != nil {
		return Repository{}, false, false, err
	}
	repositoryPath, err = normalizeAbsPath(repositoryPath)
	if err != nil {
		return Repository{}, false, false, err
	}
	if !withinRoot(rootPath, repositoryPath) {
		return Repository{}, false, false, validationErrorf("repository path must stay within root_path")
	}
	fullPath, err := filepath.Rel(rootPath, repositoryPath)
	if err != nil {
		return Repository{}, false, false, err
	}
	fullPath = cleanRelativePath(filepath.ToSlash(fullPath))
	if fullPath == "." {
		fullPath = "."
	}
	name := filepath.Base(repositoryPath)
	if name == "." || strings.TrimSpace(name) == "" {
		name = filepath.Base(root.Path)
	}
	now := time.Now().UTC()
	existing, err := s.findGenericRepository(ctx, root.ID, fullPath)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Repository{}, false, false, err
	}
	if err == nil {
		query := `UPDATE repositories SET name = ?, root_path_id = ?, local_path = ?, status = ?, discovery_source = ?, updated_at = ? WHERE id = ?`
		args := []any{name, root.ID, repositoryPath, RepositoryStatusActive, DiscoverySourceFilesystem, formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repositories SET name = $1, root_path_id = $2, local_path = $3, status = $4, discovery_source = $5, updated_at = $6 WHERE id = $7`
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			return Repository{}, false, false, err
		}
		repo, err := s.GetRepository(ctx, existing.ID)
		return repo, false, true, err
	}
	id := newID("repo")
	query := `INSERT INTO repositories (id, name, provider, provider_host, full_path, root_path_id, local_path, status, discovery_source, auto_sync_enabled, webhook_enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{id, name, RepositoryProviderGeneric, RepositoryHostLocal, fullPath, root.ID, repositoryPath, RepositoryStatusActive, DiscoverySourceFilesystem, s.boolArg(false), s.boolArg(false), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO repositories (id, name, provider, provider_host, full_path, root_path_id, local_path, status, discovery_source, auto_sync_enabled, webhook_enabled, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			existing, findErr := s.findGenericRepository(ctx, root.ID, fullPath)
			if findErr != nil {
				return Repository{}, false, false, err
			}
			query := `UPDATE repositories SET name = ?, root_path_id = ?, local_path = ?, status = ?, discovery_source = ?, updated_at = ? WHERE id = ?`
			args := []any{name, root.ID, repositoryPath, RepositoryStatusActive, DiscoverySourceFilesystem, formatTime(now), existing.ID}
			if s.handle.Provider == "postgres" {
				query = `UPDATE repositories SET name = $1, root_path_id = $2, local_path = $3, status = $4, discovery_source = $5, updated_at = $6 WHERE id = $7`
			}
			if _, updateErr := s.handle.DB.ExecContext(ctx, query, args...); updateErr != nil {
				return Repository{}, false, false, updateErr
			}
			repo, getErr := s.GetRepository(ctx, existing.ID)
			return repo, false, true, getErr
		}
		return Repository{}, false, false, err
	}
	repo, err := s.GetRepository(ctx, id)
	return repo, true, false, err
}

func (s *Store) findRepository(ctx context.Context, provider, providerHost, fullPath string) (Repository, error) {
	query := "SELECT " + s.repositoryColumns() + " FROM repositories WHERE provider = ? AND provider_host = ? AND full_path = ?"
	args := []any{provider, providerHost, fullPath}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.repositoryColumns() + " FROM repositories WHERE provider = $1 AND provider_host = $2 AND full_path = $3"
	}
	repo, err := scanRepository(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	return repo, err
}

func (s *Store) findGenericRepository(ctx context.Context, rootPathID, fullPath string) (Repository, error) {
	query := "SELECT " + s.repositoryColumns() + " FROM repositories WHERE provider = ? AND provider_host = ? AND root_path_id = ? AND full_path = ?"
	args := []any{RepositoryProviderGeneric, RepositoryHostLocal, rootPathID, fullPath}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.repositoryColumns() + " FROM repositories WHERE provider = $1 AND provider_host = $2 AND root_path_id = $3 AND full_path = $4"
	}
	repo, err := scanRepository(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	return repo, err
}

func (s *Store) GetRepository(ctx context.Context, id string) (Repository, error) {
	query := "SELECT " + s.repositoryColumns() + " FROM repositories WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.repositoryColumns() + " FROM repositories WHERE id = $1"
	}
	repo, err := scanRepository(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	return repo, err
}

func (s *Store) ListRepositories(ctx context.Context, opts RepositoryListOptions) (Page[Repository], error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.Provider != "" {
		add("provider = %s", opts.Provider)
	}
	if opts.ProviderHost != "" {
		add("provider_host = %s", opts.ProviderHost)
	}
	if opts.FullPath != "" {
		add("full_path = %s", opts.FullPath)
	}
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = RepositoryStatusActive
	}
	if status != "all" {
		if !validRepositoryStatus(status) {
			return Page[Repository]{}, validationErrorf("unsupported repository status %q", status)
		}
		add("status = %s", status)
	}
	if opts.DiscoverySource != "" {
		if !validDiscoverySource(opts.DiscoverySource) {
			return Page[Repository]{}, validationErrorf("unsupported discovery_source %q", opts.DiscoverySource)
		}
		add("discovery_source = %s", opts.DiscoverySource)
	}
	if opts.AutoSyncEnabled != nil {
		add("auto_sync_enabled = %s", s.boolArg(*opts.AutoSyncEnabled))
	}
	query := "SELECT " + s.repositoryColumns() + " FROM repositories"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanRepository)
}

func (s *Store) UpsertProjectLink(ctx context.Context, leftID, rightID, repositoryID, jobID string) (bool, error) {
	if leftID == rightID {
		return false, nil
	}
	source, target := canonicalPair(leftID, rightID)
	now := time.Now().UTC()
	id := newID("plink")
	query := `INSERT INTO project_links (id, source_project_id, target_project_id, link_type, repository_id, detected_by_job_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{id, source, target, LinkTypeSameRepository, repositoryID, nullEmpty(jobID), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO project_links (id, source_project_id, target_project_id, link_type, repository_id, detected_by_job_id, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			update := `UPDATE project_links SET repository_id = ?, detected_by_job_id = ?, updated_at = ? WHERE source_project_id = ? AND target_project_id = ? AND link_type = ?`
			updateArgs := []any{repositoryID, nullEmpty(jobID), formatTime(now), source, target, LinkTypeSameRepository}
			if s.handle.Provider == "postgres" {
				update = `UPDATE project_links SET repository_id = $1, detected_by_job_id = $2, updated_at = $3 WHERE source_project_id = $4 AND target_project_id = $5 AND link_type = $6`
			}
			_, updateErr := s.handle.DB.ExecContext(ctx, update, updateArgs...)
			return false, updateErr
		}
		return false, err
	}
	return true, nil
}

func (s *Store) ListProjectLinks(ctx context.Context, opts ProjectLinkListOptions) (Page[ProjectLink], error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.ProjectID != "" {
		args = append(args, opts.ProjectID, opts.ProjectID)
		where = append(where, fmt.Sprintf("(source_project_id = %s OR target_project_id = %s)", s.placeholder(len(args)-1), s.placeholder(len(args))))
	}
	if opts.RepositoryID != "" {
		add("repository_id = %s", opts.RepositoryID)
	}
	if opts.LinkType != "" {
		if opts.LinkType != LinkTypeSameRepository {
			return Page[ProjectLink]{}, validationErrorf("unsupported project link_type %q", opts.LinkType)
		}
		add("link_type = %s", opts.LinkType)
	}
	query := "SELECT " + s.projectLinkColumns() + " FROM project_links"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanProjectLink)
}

func (s *Store) IgnoreRulesForRoot(ctx context.Context, rootPathID string) ([]IgnoreRule, error) {
	query := "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules WHERE scope_type = ? OR (scope_type = ? AND scope_id = ?) ORDER BY sort_order ASC, created_at ASC, id ASC"
	args := []any{"system", "root_path", rootPathID}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules WHERE scope_type = $1 OR (scope_type = $2 AND scope_id = $3) ORDER BY sort_order ASC, created_at ASC, id ASC"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IgnoreRule
	for rows.Next() {
		item, err := scanIgnoreRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListIgnoreRules(ctx context.Context, opts IgnoreRuleListOptions) (Page[IgnoreRule], error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.ScopeType != "" {
		add("scope_type = %s", opts.ScopeType)
	}
	if opts.ScopeID != "" {
		add("scope_id = %s", opts.ScopeID)
	}
	query := "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanIgnoreRule)
}

func (s *Store) UpsertIgnoreRules(ctx context.Context, inputs []IgnoreRuleInput) ([]IgnoreRule, error) {
	out := make([]IgnoreRule, 0, len(inputs))
	for idx, input := range inputs {
		item, err := s.upsertIgnoreRule(ctx, input, idx)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) upsertIgnoreRule(ctx context.Context, input IgnoreRuleInput, defaultSortOrder int) (IgnoreRule, error) {
	scopeType := strings.TrimSpace(input.ScopeType)
	if !validIgnoreScope(scopeType) {
		return IgnoreRule{}, fmt.Errorf("unsupported ignore rule scope_type %q", scopeType)
	}
	scopeID := strings.TrimSpace(input.ScopeID)
	if scopeType != "system" && scopeID == "" {
		return IgnoreRule{}, fmt.Errorf("scope_id is required for %s ignore rules", scopeType)
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return IgnoreRule{}, fmt.Errorf("ignore rule pattern is required")
	}
	origin := strings.TrimSpace(input.Origin)
	if origin == "" {
		origin = "ui"
	}
	if !validIgnoreOrigin(origin) {
		return IgnoreRule{}, fmt.Errorf("unsupported ignore rule origin %q", origin)
	}
	sortOrder := defaultSortOrder
	if input.SortOrder != nil {
		sortOrder = *input.SortOrder
	}
	existing, err := s.findIgnoreRule(ctx, input.ID, scopeType, scopeID, pattern)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return IgnoreRule{}, err
	}
	now := time.Now().UTC()
	if err == nil {
		query := "UPDATE ignore_rules SET origin = ?, sort_order = ?, updated_at = ? WHERE id = ?"
		args := []any{origin, sortOrder, formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = "UPDATE ignore_rules SET origin = $1, sort_order = $2, updated_at = $3 WHERE id = $4"
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			return IgnoreRule{}, err
		}
		return s.GetIgnoreRule(ctx, existing.ID)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = newID("ignore")
	}
	query := `INSERT INTO ignore_rules (id, scope_type, scope_id, pattern, origin, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{id, scopeType, scopeID, pattern, origin, sortOrder, formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO ignore_rules (id, scope_type, scope_id, pattern, origin, sort_order, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			existing, findErr := s.findIgnoreRule(ctx, "", scopeType, scopeID, pattern)
			if findErr != nil {
				return IgnoreRule{}, err
			}
			return s.GetIgnoreRule(ctx, existing.ID)
		}
		return IgnoreRule{}, err
	}
	return s.GetIgnoreRule(ctx, id)
}

func (s *Store) findIgnoreRule(ctx context.Context, id, scopeType, scopeID, pattern string) (IgnoreRule, error) {
	if id != "" {
		item, err := s.GetIgnoreRule(ctx, id)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return item, err
		}
	}
	query := "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules WHERE scope_type = ? AND scope_id = ? AND pattern = ?"
	args := []any{scopeType, scopeID, pattern}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules WHERE scope_type = $1 AND scope_id = $2 AND pattern = $3"
	}
	item, err := scanIgnoreRule(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return IgnoreRule{}, ErrNotFound
	}
	return item, err
}

func (s *Store) GetIgnoreRule(ctx context.Context, id string) (IgnoreRule, error) {
	query := "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.ignoreRuleColumns() + " FROM ignore_rules WHERE id = $1"
	}
	item, err := scanIgnoreRule(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return IgnoreRule{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListEnvironments(ctx context.Context, opts ListOptions) (Page[Environment], error) {
	query := "SELECT " + s.environmentColumns() + " FROM environments"
	return listPage(ctx, s, query, nil, "created_at", opts, scanEnvironment)
}

func (s *Store) GetEnvironment(ctx context.Context, id string) (Environment, error) {
	query := "SELECT " + s.environmentColumns() + " FROM environments WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.environmentColumns() + " FROM environments WHERE id = $1"
	}
	item, err := scanEnvironment(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListWorkspaces(ctx context.Context, opts WorkspaceListOptions) (Page[Workspace], error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.ProjectID != "" {
		add("project_id = %s", opts.ProjectID)
	}
	if opts.EnvironmentID != "" {
		add("environment_id = %s", opts.EnvironmentID)
	}
	query := "SELECT " + s.workspaceColumns() + " FROM workspaces"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	return listPage(ctx, s, query, args, "created_at", opts.ListOptions, scanWorkspace)
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	query := "SELECT " + s.workspaceColumns() + " FROM workspaces WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.workspaceColumns() + " FROM workspaces WHERE id = $1"
	}
	item, err := scanWorkspace(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	return item, err
}

func listPage[T any](ctx context.Context, s *Store, baseQuery string, args []any, timeColumn string, opts ListOptions, scan func(interface{ Scan(dest ...any) error }) (T, error)) (Page[T], error) {
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := baseQuery
	if opts.Cursor != "" {
		cursor, err := decodeCursor(opts.Cursor)
		if err != nil {
			return Page[T]{}, err
		}
		clause := fmt.Sprintf("(%s < %s OR (%s = %s AND id < %s))", timeColumn, s.placeholder(len(args)+1), timeColumn, s.placeholder(len(args)+2), s.placeholder(len(args)+3))
		if strings.Contains(strings.ToUpper(query), " WHERE ") {
			query += " AND " + clause
		} else {
			query += " WHERE " + clause
		}
		args = append(args, formatTime(cursor.Time), formatTime(cursor.Time), cursor.ID)
	}
	args = append(args, limit+1)
	query += " ORDER BY " + timeColumn + " DESC, id DESC LIMIT " + s.placeholder(len(args))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[T]{}, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return Page[T]{}, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return Page[T]{}, err
	}
	var next string
	if len(out) > limit {
		out = out[:limit]
		next = encodeCursor(valueCreatedAt(out[len(out)-1]), valueID(out[len(out)-1]))
	}
	return Page[T]{Items: out, NextCursor: next}, nil
}

func valueID(value any) string {
	switch v := value.(type) {
	case RootPath:
		return v.ID
	case Project:
		return v.ID
	case ProjectLink:
		return v.ID
	case Repository:
		return v.ID
	case IgnoreRule:
		return v.ID
	case Environment:
		return v.ID
	case Workspace:
		return v.ID
	default:
		return ""
	}
}

func valueCreatedAt(value any) time.Time {
	switch v := value.(type) {
	case RootPath:
		return v.CreatedAt
	case Project:
		return v.CreatedAt
	case ProjectLink:
		return v.CreatedAt
	case Repository:
		return v.CreatedAt
	case IgnoreRule:
		return v.CreatedAt
	case Environment:
		return v.CreatedAt
	case Workspace:
		return v.CreatedAt
	default:
		return time.Time{}
	}
}

func (s *Store) rootPathColumns() string {
	return fmt.Sprintf(`id, name, path, source, %s, %s, COALESCE(schedule_frequency, ''), %s, %s`,
		s.boolSelect("enabled"), s.boolSelect("schedule_enabled"), s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func (s *Store) projectColumns() string {
	return fmt.Sprintf(`id, name, path, relative_path, root_path_id, terraform_marker, status, COALESCE(repository_id, ''), COALESCE(environment_id, ''), COALESCE(default_workspace_id, ''), %s, %s, %s, %s`,
		s.timeExpr("detected_at"), s.timeExpr("last_seen_at"), s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func (s *Store) projectLinkColumns() string {
	return fmt.Sprintf(`id, source_project_id, target_project_id, link_type, COALESCE(repository_id, ''), COALESCE(detected_by_job_id, ''), %s, %s`,
		s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func (s *Store) repositoryColumns() string {
	return fmt.Sprintf(`id, name, COALESCE(provider_instance_id, ''), provider, provider_host, full_path, COALESCE(clone_url, ''), COALESCE(default_branch, ''), COALESCE(root_path_id, ''), COALESCE(target_directory, ''), COALESCE(local_path, ''), COALESCE(auth_type, ''), COALESCE(default_credential_id, ''), status, discovery_source, COALESCE(superseded_by_repository_id, ''), COALESCE(%s, ''), %s, %s, COALESCE(poll_interval, ''), COALESCE(%s, ''), COALESCE(last_error, ''), %s, %s`,
		s.timeExpr("identity_confirmed_at"), s.boolSelect("auto_sync_enabled"), s.boolSelect("webhook_enabled"), s.timeExpr("last_pull_at"), s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func (s *Store) ignoreRuleColumns() string {
	return fmt.Sprintf(`id, scope_type, COALESCE(scope_id, ''), pattern, origin, sort_order, %s, %s`,
		s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func (s *Store) environmentColumns() string {
	return fmt.Sprintf(`id, name, code, COALESCE(description, ''), %s, %s`, s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func (s *Store) workspaceColumns() string {
	return fmt.Sprintf(`id, project_id, environment_id, name, %s, %s, %s`,
		s.boolSelect("is_default"), s.timeExpr("created_at"), s.timeExpr("updated_at"))
}

func scanRootPath(row interface{ Scan(dest ...any) error }) (RootPath, error) {
	var item RootPath
	var enabled, schedule int
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.Path, &item.Source, &enabled, &schedule, &item.ScheduleFrequency, &created, &updated); err != nil {
		return RootPath{}, err
	}
	item.Enabled = enabled != 0
	item.ScheduleEnabled = schedule != 0
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanProject(row interface{ Scan(dest ...any) error }) (Project, error) {
	var item Project
	var detected, lastSeen, created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.Path, &item.RelativePath, &item.RootPathID, &item.TerraformMarker, &item.Status, &item.RepositoryID, &item.EnvironmentID, &item.DefaultWorkspaceID, &detected, &lastSeen, &created, &updated); err != nil {
		return Project{}, err
	}
	item.DetectedAt, _ = parseTime(detected)
	item.LastSeenAt, _ = parseTime(lastSeen)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanProjectLink(row interface{ Scan(dest ...any) error }) (ProjectLink, error) {
	var item ProjectLink
	var created, updated string
	if err := row.Scan(&item.ID, &item.SourceProjectID, &item.TargetProjectID, &item.LinkType, &item.RepositoryID, &item.DetectedByJobID, &created, &updated); err != nil {
		return ProjectLink{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanRepository(row interface{ Scan(dest ...any) error }) (Repository, error) {
	var item Repository
	var identityConfirmed, lastPull, created, updated string
	var autoSync, webhook int
	if err := row.Scan(&item.ID, &item.Name, &item.ProviderInstanceID, &item.Provider, &item.ProviderHost, &item.FullPath, &item.CloneURL, &item.DefaultBranch, &item.RootPathID, &item.TargetDirectory, &item.LocalPath, &item.AuthType, &item.DefaultCredentialID, &item.Status, &item.DiscoverySource, &item.SupersededByRepositoryID, &identityConfirmed, &autoSync, &webhook, &item.PollInterval, &lastPull, &item.LastError, &created, &updated); err != nil {
		return Repository{}, err
	}
	item.IdentityConfirmedAt = parseTimePtr(identityConfirmed)
	item.AutoSyncEnabled = autoSync != 0
	item.WebhookEnabled = webhook != 0
	item.LastPullAt = parseTimePtr(lastPull)
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanIgnoreRule(row interface{ Scan(dest ...any) error }) (IgnoreRule, error) {
	var item IgnoreRule
	var created, updated string
	if err := row.Scan(&item.ID, &item.ScopeType, &item.ScopeID, &item.Pattern, &item.Origin, &item.SortOrder, &created, &updated); err != nil {
		return IgnoreRule{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanEnvironment(row interface{ Scan(dest ...any) error }) (Environment, error) {
	var item Environment
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.Code, &item.Description, &created, &updated); err != nil {
		return Environment{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func scanWorkspace(row interface{ Scan(dest ...any) error }) (Workspace, error) {
	var item Workspace
	var isDefault int
	var created, updated string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.EnvironmentID, &item.Name, &isDefault, &created, &updated); err != nil {
		return Workspace{}, err
	}
	item.IsDefault = isDefault != 0
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

func (s *Store) placeholder(idx int) string {
	return s.handle.Dialect().Placeholder(idx)
}

func (s *Store) timeExpr(column string) string {
	return s.handle.Dialect().TimeExpr(column)
}

func (s *Store) boolSelect(column string) string {
	return "CASE WHEN " + column + " THEN 1 ELSE 0 END"
}

func (s *Store) boolArg(value bool) any {
	return s.handle.Dialect().BoolArg(value)
}

func normalizeAbsPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	cleaned := filepath.Clean(value)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be absolute")
	}
	return cleaned, nil
}

func cleanRelativePath(value string) string {
	value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	if value == "" || value == "/" {
		return "."
	}
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return "."
	}
	return value
}

func safeRelativePath(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ".", nil
	}
	slashPath := strings.ReplaceAll(filepath.ToSlash(raw), "\\", "/")
	if filepath.IsAbs(raw) || strings.HasPrefix(slashPath, "/") || looksLikeWindowsAbsPath(slashPath) {
		return "", validationErrorf("relative_path must be relative")
	}
	cleaned := cleanRelativePath(slashPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", validationErrorf("relative_path must stay within root_path")
	}
	return cleaned, nil
}

func looksLikeWindowsAbsPath(value string) bool {
	if len(value) < 3 || value[1] != ':' || value[2] != '/' {
		return false
	}
	return (value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')
}

func underErroredSubtree(relativePath string, erroredSubtrees map[string]bool) bool {
	if len(erroredSubtrees) == 0 {
		return false
	}
	relativePath = cleanRelativePath(relativePath)
	for subtree := range erroredSubtrees {
		subtree = cleanRelativePath(subtree)
		if subtree == "." || relativePath == subtree || strings.HasPrefix(relativePath, subtree+"/") {
			return true
		}
	}
	return false
}

func batches(values []string, size int) [][]string {
	if len(values) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(values)
	}
	out := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		out = append(out, values[start:end])
	}
	return out
}

func validateFrequency(value string) error {
	switch value {
	case "", "daily", "weekly", "monthly":
		return nil
	default:
		return fmt.Errorf("unsupported schedule_frequency %q", value)
	}
}

func validProjectStatus(value string) bool {
	switch value {
	case ProjectStatusActive, ProjectStatusMissing, ProjectStatusDisabled:
		return true
	default:
		return false
	}
}

func validRepositoryStatus(value string) bool {
	switch value {
	case RepositoryStatusActive, "missing", "superseded", "disabled":
		return true
	default:
		return false
	}
}

func validDiscoverySource(value string) bool {
	switch value {
	case DiscoverySourceFilesystem, "provider", "clone", "manual":
		return true
	default:
		return false
	}
}

func validIgnoreScope(value string) bool {
	switch value {
	case "system", "root_path", "project":
		return true
	default:
		return false
	}
}

func validIgnoreOrigin(value string) bool {
	switch value {
	case "ui", "config_import", "system_default":
		return true
	default:
		return false
	}
}

func canonicalPair(left, right string) (string, string) {
	if left < right {
		return left, right
	}
	return right, left
}

func nullEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999-07", value); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999Z07:00", value); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, value)
}

func parseTimePtr(value string) *time.Time {
	t, err := parseTime(value)
	if err != nil || t.IsZero() {
		return nil
	}
	return &t
}

type listCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func encodeCursor(t time.Time, id string) string {
	raw, _ := json.Marshal(listCursor{Time: t.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(value string) (listCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	var cursor listCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	if cursor.Time.IsZero() || cursor.ID == "" {
		return listCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "duplicate key value") ||
		strings.Contains(message, "constraint failed: unique") ||
		strings.Contains(message, "sqlite_constraint_unique")
}

func validationErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}
