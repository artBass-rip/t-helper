package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

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
