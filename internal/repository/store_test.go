package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
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

func TestRepositoryStoreContractPostgres(t *testing.T) {
	dsn := os.Getenv("THELPER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("THELPER_POSTGRES_DSN is not set")
	}
	requireRepositoryPostgresTestDatabase(t, dsn)
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	handle, err := registry.Open(ctx, storage.Config{Provider: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer handle.Close()
	resetRepositoryPostgresTables(t, handle.DB)
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	generic, _, _, err := scannerStore.UpsertGenericRepository(ctx, root, filepath.Join(root.Path, "repo"))
	if err != nil {
		t.Fatalf("upsert generic repository: %v", err)
	}
	project, _, err := scannerStore.UpsertProject(ctx, root, "repo/app", time.Now().UTC())
	if err != nil {
		t.Fatalf("upsert project: %v", err)
	}
	if err := scannerStore.SetProjectRepository(ctx, project.ID, generic.ID); err != nil {
		t.Fatalf("link project repository: %v", err)
	}
	canonical, _, err := repoStore.UpsertRepositoryForClone(ctx, Identity{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		FullPath:     "example/repo",
		CloneURL:     "https://github.com/example/repo.git",
		Protocol:     ProtocolHTTPS,
	}, root, "repo", filepath.Join(root.Path, "repo"), "", "")
	if err != nil {
		t.Fatalf("upsert provider-aware repository: %v", err)
	}
	if canonical.ID != generic.ID || canonical.IdentityConfirmedAt == nil {
		t.Fatalf("repository was not enriched in place: canonical=%+v generic=%+v", canonical, generic)
	}
	if _, err := repoStore.ReserveOperationKeys(ctx, "owner-one", time.Minute, IdentityReservationKey(ProviderGitHub, "github.com", "example/repo")); err != nil {
		t.Fatalf("reserve operation key: %v", err)
	}
	if _, err := repoStore.ReserveOperationKeys(ctx, "owner-two", time.Minute, IdentityReservationKey(ProviderGitHub, "github.com", "example/repo")); !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("second reservation error = %v, want %v", err, ErrReservationConflict)
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
	var conflict ReservationConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("reserve second error = %v, want reservation conflict", err)
	}
	if conflict.Key != "repository-path:root:repo" || conflict.Owner != "owner-one" {
		t.Fatalf("reservation conflict = %+v, want key and owning reservation", conflict)
	}
	if err := store.ReleaseOperationReservations(ctx, "owner-one", held...); err != nil {
		t.Fatalf("release first: %v", err)
	}
	if _, err := store.ReserveOperationKeys(ctx, "owner-two", time.Minute, "repository-path:root:repo"); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
}

func TestReserveOperationKeysAllowsOnlyOneConcurrentOwner(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	const workers = 8
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for idx := 0; idx < workers; idx++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := store.ReserveOperationKeys(ctx, "owner-concurrent-"+string(rune('a'+idx)), time.Minute, "repository-identity:github:github.com:example/repo")
			results <- err
		}(idx)
	}
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrReservationConflict):
			conflicts++
		default:
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	if successes != 1 || conflicts != workers-1 {
		t.Fatalf("reservation results: successes=%d conflicts=%d, want one winner", successes, conflicts)
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

func TestReserveOperationKeysPrunesOldReleasedReservations(t *testing.T) {
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	store := NewStore(handle)
	old := time.Now().UTC().Add(-2 * operationReservationRetention)
	_, err := handle.DB.ExecContext(ctx, `INSERT INTO repository_operation_reservations (id, reservation_key, owner, status, created_at, expires_at, released_at) VALUES (?, ?, ?, 'released', ?, ?, ?)`,
		"rres_old", "repository-path:root:old", "owner-old", formatTime(old), formatTime(old), formatTime(old))
	if err != nil {
		t.Fatalf("insert old reservation: %v", err)
	}
	if _, err := store.ReserveOperationKeys(ctx, "owner-new", time.Minute, "repository-path:root:new"); err != nil {
		t.Fatalf("reserve new key: %v", err)
	}
	var count int
	if err := handle.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM repository_operation_reservations WHERE id = ?`, "rres_old").Scan(&count); err != nil {
		t.Fatalf("count old reservation: %v", err)
	}
	if count != 0 {
		t.Fatalf("old released reservation count = %d, want 0", count)
	}
}

func TestProviderInstanceListUsesCursorPagination(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	for _, input := range []ProviderInstanceInput{
		{ID: "rpi_a", Provider: ProviderGitHub, ProviderHost: "ghe-a.example.internal"},
		{ID: "rpi_b", Provider: ProviderGitHub, ProviderHost: "ghe-b.example.internal"},
		{ID: "rpi_c", Provider: ProviderGitHub, ProviderHost: "ghe-c.example.internal"},
	} {
		if _, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{input}); err != nil {
			t.Fatalf("upsert provider instance %s: %v", input.ID, err)
		}
	}
	first, err := store.ListProviderInstancesPage(ctx, ProviderInstanceListOptions{ListOptions: ListOptions{Limit: 2}})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v, want 2 items and next cursor", first)
	}
	second, err := store.ListProviderInstancesPage(ctx, ProviderInstanceListOptions{ListOptions: ListOptions{Limit: 2, Cursor: first.NextCursor}})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page = %+v, want 1 item and no next cursor", second)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.ID] {
			t.Fatalf("provider instance repeated across pages: %s", item.ID)
		}
		seen[item.ID] = true
	}
	if _, err := store.ListProviderInstancesPage(ctx, ProviderInstanceListOptions{ListOptions: ListOptions{Cursor: "not-a-cursor"}}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor error = %v, want %v", err, ErrInvalidCursor)
	}
}

func TestCredentialListFiltersUsageBeforeLimitAndUsesCursor(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	enabled := true
	instances, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		Enabled:      &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	inputs := []CredentialInput{
		{ID: "rcred_webhook", ProviderInstanceID: instances[0].ID, Name: "webhook", AuthType: AuthTypeWebhookSecret, SecretRef: "secretref://env/WEBHOOK_SECRET", Usages: []string{UsageWebhook}, Enabled: &enabled},
		{ID: "rcred_git_one", ProviderInstanceID: instances[0].ID, Name: "git-one", AuthType: AuthTypeHTTPSToken, SecretRef: "secretref://env/GITHUB_TOKEN_ONE", Usages: []string{UsageGitTransport}, Enabled: &enabled},
		{ID: "rcred_git_two", ProviderInstanceID: instances[0].ID, Name: "git-two", AuthType: AuthTypeHTTPSToken, SecretRef: "secretref://env/GITHUB_TOKEN_TWO", Usages: []string{UsageGitTransport}, Enabled: &enabled},
	}
	for _, input := range inputs {
		if _, err := store.UpsertCredentials(ctx, []CredentialInput{input}); err != nil {
			t.Fatalf("upsert credential %s: %v", input.ID, err)
		}
	}
	webhook, err := store.ListCredentialsPage(ctx, CredentialListOptions{
		ListOptions:        ListOptions{Limit: 1},
		ProviderInstanceID: instances[0].ID,
		Usage:              UsageWebhook,
	})
	if err != nil {
		t.Fatalf("list webhook credentials: %v", err)
	}
	if len(webhook.Items) != 1 || webhook.Items[0].ID != "rcred_webhook" || webhook.Items[0].SecretRef != "secretref://env/***" {
		t.Fatalf("webhook page = %+v, want masked webhook credential", webhook)
	}
	first, err := store.ListCredentialsPage(ctx, CredentialListOptions{
		ListOptions:        ListOptions{Limit: 1},
		ProviderInstanceID: instances[0].ID,
		Usage:              UsageGitTransport,
	})
	if err != nil {
		t.Fatalf("list first git credential page: %v", err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first git credential page = %+v, want 1 item and next cursor", first)
	}
	second, err := store.ListCredentialsPage(ctx, CredentialListOptions{
		ListOptions:        ListOptions{Limit: 1, Cursor: first.NextCursor},
		ProviderInstanceID: instances[0].ID,
		Usage:              UsageGitTransport,
	})
	if err != nil {
		t.Fatalf("list second git credential page: %v", err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second git credential page = %+v after first %+v", second, first)
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

func TestCredentialValidationRejectsProtocolMismatch(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
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
		Name:               "ssh",
		AuthType:           AuthTypeSSHKey,
		SecretRef:          "secretref://env/GITHUB_SSH_KEY",
		Usages:             []string{UsageGitTransport},
		Enabled:            &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
	if err := store.ValidateCredentialForProtocol(ctx, credentials[0].ID, instances[0].ID, UsageGitTransport, ProtocolSSH); err != nil {
		t.Fatalf("ssh credential should be valid for ssh protocol: %v", err)
	}
	err = store.ValidateCredentialForProtocol(ctx, credentials[0].ID, instances[0].ID, UsageGitTransport, ProtocolHTTPS)
	if err == nil {
		t.Fatal("expected ssh credential to reject https protocol")
	}
	if code := ValidationCode(err); code != "credential_auth_type_protocol_mismatch" {
		t.Fatalf("validation code = %q", code)
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

func TestProviderInstanceValidatesProfileURLs(t *testing.T) {
	ctx := context.Background()
	store := NewStore(openRepositorySQLite(t))
	instances, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{{
		Provider:     ProviderGitHub,
		ProviderHost: "ghe.example.internal:443",
		APIBaseURL:   "https://ghe.example.internal/api/v3",
		WebBaseURL:   "https://ghe.example.internal/",
	}})
	if err != nil {
		t.Fatalf("upsert provider instance: %v", err)
	}
	if instances[0].ProviderHost != "ghe.example.internal" || instances[0].APIBaseURL != "https://ghe.example.internal/api/v3" {
		t.Fatalf("unexpected normalized provider instance: %+v", instances[0])
	}

	for _, tc := range []ProviderInstanceInput{
		{Provider: ProviderGitHub, ProviderHost: "github.com", APIBaseURL: "http://github.com/api/v3"},
		{Provider: ProviderGitHub, ProviderHost: "github.com", APIBaseURL: "https://token@github.com/api/v3"},
		{Provider: ProviderGitHub, ProviderHost: "github.com", APIBaseURL: "https://github.com/api/v3?token=secret"},
		{Provider: ProviderGitHub, ProviderHost: "github.com", APIBaseURL: "https://github.com/api/v3#fragment"},
		{Provider: ProviderGitHub, ProviderHost: "github.com", APIBaseURL: "https://github.com/api/\x01"},
		{Provider: ProviderGitHub, ProviderHost: "github.com", WebBaseURL: "https://ghe.example.internal/"},
	} {
		if _, err := store.UpsertProviderInstances(ctx, []ProviderInstanceInput{tc}); err == nil {
			t.Fatalf("expected provider profile URL validation error for %+v", tc)
		}
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

func TestGitCommandEnvIsNonInteractiveAndRepositoryMessagesAreRedacted(t *testing.T) {
	env := gitCommandEnv(nil)
	hasPromptOff := false
	hasCredentialManagerOff := false
	for _, item := range env {
		switch item {
		case "GIT_TERMINAL_PROMPT=0":
			hasPromptOff = true
		case "GCM_INTERACTIVE=Never":
			hasCredentialManagerOff = true
		}
	}
	if !hasPromptOff || !hasCredentialManagerOff {
		t.Fatalf("git env missing non-interactive settings: prompt=%v credential_manager=%v", hasPromptOff, hasCredentialManagerOff)
	}
	message := redactRepositoryMessage("failed token=abc123 https://user:pass@example.test/repo.git secretref://env/API_TOKEN")
	for _, leaked := range []string{"abc123", "user:pass", "API_TOKEN"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("redacted message leaked %q: %s", leaked, message)
		}
	}
	if !strings.Contains(message, "[redacted]") {
		t.Fatalf("expected redaction marker in %q", message)
	}
}

func TestGitCredentialEnvUsesNonInteractiveSSHCommand(t *testing.T) {
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
		Name:               "ssh",
		AuthType:           AuthTypeSSHKey,
		SecretRef:          "secretref://env/REPOSITORY_SSH_KEY",
		Usages:             []string{UsageGitTransport},
		Enabled:            &enabled,
	}})
	if err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
	t.Setenv("REPOSITORY_SSH_KEY", "test-private-key")
	env, cleanup, err := (operationHandler{store: store, scannerStore: scannerStore}).gitCredentialEnv(ctx, credentials[0].ID)
	defer cleanup()
	if err != nil {
		t.Fatalf("credential env: %v", err)
	}
	if len(env) != 1 || !strings.Contains(env[0], "GIT_SSH_COMMAND=ssh ") || !strings.Contains(env[0], "-o BatchMode=yes") || !strings.Contains(env[0], "-o IdentitiesOnly=yes") {
		t.Fatalf("unexpected ssh credential env: %+v", env)
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
	_, localPath, err := NormalizeTarget(root.Path, targetDirectory)
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
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
	identityReservationKey := IdentityReservationKey(repo.Provider, repo.ProviderHost, repo.FullPath)
	pathReservationKey, err := TargetReservationKey(root.Path, root.ID, targetDirectory)
	if err != nil {
		t.Fatalf("target reservation key: %v", err)
	}
	if _, err := repoStore.ReserveOperationKeys(ctx, "job_clone_git", time.Hour, identityReservationKey, pathReservationKey); err != nil {
		t.Fatalf("reserve operation keys before worker: %v", err)
	}
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
	reloaded, err := scannerStore.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("reload repository: %v", err)
	}
	if reloaded.DefaultBranch == "" || reloaded.LastError != "" || reloaded.LastPullAt == nil {
		t.Fatalf("repository pull metadata was not updated: %+v", reloaded)
	}
	if held := countHeldOperationReservations(t, ctx, repoStore); held != 0 {
		t.Fatalf("held operation reservations after clone = %d, want 0", held)
	}
}

func TestOperationHandlerCloneUsesExistingEmptyTargetDirectory(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	source := createGitFixtureRepository(t)
	targetDirectory := "empty/repo"
	_, localPath, err := NormalizeTarget(root.Path, targetDirectory)
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		t.Fatalf("mkdir empty target: %v", err)
	}
	identity, err := NormalizeIdentity(CloneRequest{Provider: ProviderGeneric, Protocol: ProtocolHTTPS, CloneURL: source, CloneScope: "single_repository"}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	repo, err := repoStore.UpsertRepository(ctx, identity, root, targetDirectory, localPath, "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	payload, _ := json.Marshal(RepoClonePayload{
		SchemaVersion:   RepoClonePayloadSchema,
		RepositoryID:    repo.ID,
		Provider:        identity.Provider,
		ProviderHost:    identity.ProviderHost,
		Protocol:        identity.Protocol,
		CloneURL:        identity.CloneURL,
		CloneScope:      "single_repository",
		FullPath:        identity.FullPath,
		RootPathID:      root.ID,
		TargetDirectory: targetDirectory,
		LocalPath:       localPath,
	})
	if _, err := (operationHandler{store: repoStore, scannerStore: scannerStore}).handleClone(ctx, jobs.Job{ID: "job_clone_empty", JobType: "repo_clone", Payload: payload}); err != nil {
		t.Fatalf("handle clone into empty target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localPath, ".git")); err != nil {
		t.Fatalf("cloned working tree missing .git: %v", err)
	}
}

func TestOperationHandlerCloneRejectsSymlinkEscapeChangedAfterEnqueue(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	if err := os.MkdirAll(root.Path, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	source := createGitFixtureRepository(t)
	targetDirectory := "linked/repo"
	_, localPath, err := NormalizeTarget(root.Path, targetDirectory)
	if err != nil {
		t.Fatalf("normalize target before symlink: %v", err)
	}
	identity, err := NormalizeIdentity(CloneRequest{Provider: ProviderGeneric, Protocol: ProtocolHTTPS, CloneURL: source, CloneScope: "single_repository"}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	repo, err := repoStore.UpsertRepository(ctx, identity, root, targetDirectory, localPath, "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root.Path, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	payload, _ := json.Marshal(RepoClonePayload{
		SchemaVersion:   RepoClonePayloadSchema,
		RepositoryID:    repo.ID,
		Provider:        identity.Provider,
		ProviderHost:    identity.ProviderHost,
		Protocol:        identity.Protocol,
		CloneURL:        identity.CloneURL,
		CloneScope:      "single_repository",
		FullPath:        identity.FullPath,
		RootPathID:      root.ID,
		TargetDirectory: targetDirectory,
		LocalPath:       localPath,
	})
	_, err = (operationHandler{store: repoStore, scannerStore: scannerStore}).Handle(ctx, jobs.HandlerEnv{}, jobs.Job{ID: "job_clone_symlink_escape", JobType: "repo_clone", Payload: payload})
	var handlerErr jobs.HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "validation_error" {
		t.Fatalf("clone symlink escape error = %v, want validation_error", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "repo")); !os.IsNotExist(err) {
		t.Fatalf("outside target stat error = %v, want not exist", err)
	}
}

func TestOperationHandlerCloneExistingExpectedRemoteRunsPull(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	source := createGitFixtureRepository(t)
	targetDirectory := "existing/repo"
	_, localPath, err := NormalizeTarget(root.Path, targetDirectory)
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	runGitCommand(t, "", "clone", source, localPath)
	mustWriteRepositoryTestFile(t, filepath.Join(source, "CHANGELOG.md"), "change\n")
	runGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "add", "CHANGELOG.md")
	runGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "commit", "-m", "change")
	identity, err := NormalizeIdentity(CloneRequest{Provider: ProviderGeneric, Protocol: ProtocolHTTPS, CloneURL: source, CloneScope: "single_repository"}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	repo, err := repoStore.UpsertRepository(ctx, identity, root, targetDirectory, localPath, "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	payload, _ := json.Marshal(RepoClonePayload{
		SchemaVersion:   RepoClonePayloadSchema,
		RepositoryID:    repo.ID,
		Provider:        identity.Provider,
		ProviderHost:    identity.ProviderHost,
		Protocol:        identity.Protocol,
		CloneURL:        identity.CloneURL,
		CloneScope:      "single_repository",
		FullPath:        identity.FullPath,
		RootPathID:      root.ID,
		TargetDirectory: targetDirectory,
		LocalPath:       localPath,
	})
	result, err := (operationHandler{store: repoStore, scannerStore: scannerStore}).handleClone(ctx, jobs.Job{ID: "job_clone_existing", JobType: "repo_clone", Payload: payload})
	if err != nil {
		t.Fatalf("handle clone existing: %v", err)
	}
	var op OperationResult
	if err := json.Unmarshal(result, &op); err != nil {
		t.Fatalf("decode operation result: %v", err)
	}
	if op.Operation != "repo_clone" || !op.Changed {
		t.Fatalf("expected clone request to run pull and change checkout, got %+v", op)
	}
	if _, err := os.Stat(filepath.Join(localPath, "CHANGELOG.md")); err != nil {
		t.Fatalf("pulled file missing: %v", err)
	}
}

func TestOperationHandlerCloneExistingDifferentRemoteRejects(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	sourceOne := createGitFixtureRepository(t)
	sourceTwo := createGitFixtureRepository(t)
	targetDirectory := "existing/repo"
	_, localPath, err := NormalizeTarget(root.Path, targetDirectory)
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	runGitCommand(t, "", "clone", sourceOne, localPath)
	identity, err := NormalizeIdentity(CloneRequest{Provider: ProviderGeneric, Protocol: ProtocolHTTPS, CloneURL: sourceTwo, CloneScope: "single_repository"}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	repo, err := repoStore.UpsertRepository(ctx, identity, root, targetDirectory, localPath, "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	payload, _ := json.Marshal(RepoClonePayload{
		SchemaVersion:   RepoClonePayloadSchema,
		RepositoryID:    repo.ID,
		Provider:        identity.Provider,
		ProviderHost:    identity.ProviderHost,
		Protocol:        identity.Protocol,
		CloneURL:        identity.CloneURL,
		CloneScope:      "single_repository",
		FullPath:        identity.FullPath,
		RootPathID:      root.ID,
		TargetDirectory: targetDirectory,
		LocalPath:       localPath,
	})
	_, err = (operationHandler{store: repoStore, scannerStore: scannerStore}).Handle(ctx, jobs.HandlerEnv{}, jobs.Job{ID: "job_clone_mismatch", JobType: "repo_clone", Payload: payload})
	var handlerErr jobs.HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "repository_remote_mismatch" {
		t.Fatalf("clone mismatch error = %v, want repository_remote_mismatch", err)
	}
	reloaded, err := scannerStore.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("reload repository: %v", err)
	}
	if !strings.Contains(reloaded.LastError, "repository_remote_mismatch") {
		t.Fatalf("last_error = %q, want repository_remote_mismatch", reloaded.LastError)
	}
}

func TestOperationHandlerCloneNonEmptyTargetRejectsAndRecordsLastError(t *testing.T) {
	requireGit(t)
	ctx := context.Background()
	handle := openRepositorySQLite(t)
	scannerStore := scanner.NewStore(handle)
	repoStore := NewStore(handle)
	root := upsertRepositoryRoot(t, ctx, scannerStore)
	source := createGitFixtureRepository(t)
	targetDirectory := "busy/repo"
	_, localPath, err := NormalizeTarget(root.Path, targetDirectory)
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	mustWriteRepositoryTestFile(t, filepath.Join(localPath, "README.md"), "already here\n")
	identity, err := NormalizeIdentity(CloneRequest{Provider: ProviderGeneric, Protocol: ProtocolHTTPS, CloneURL: source, CloneScope: "single_repository"}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	repo, err := repoStore.UpsertRepository(ctx, identity, root, targetDirectory, localPath, "", "")
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	payload, _ := json.Marshal(RepoClonePayload{
		SchemaVersion:   RepoClonePayloadSchema,
		RepositoryID:    repo.ID,
		Provider:        identity.Provider,
		ProviderHost:    identity.ProviderHost,
		Protocol:        identity.Protocol,
		CloneURL:        identity.CloneURL,
		CloneScope:      "single_repository",
		FullPath:        identity.FullPath,
		RootPathID:      root.ID,
		TargetDirectory: targetDirectory,
		LocalPath:       localPath,
	})
	_, err = (operationHandler{store: repoStore, scannerStore: scannerStore}).Handle(ctx, jobs.HandlerEnv{}, jobs.Job{ID: "job_clone_busy", JobType: "repo_clone", Payload: payload})
	var handlerErr jobs.HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "repository_target_not_empty" {
		t.Fatalf("clone busy target error = %v, want repository_target_not_empty", err)
	}
	reloaded, err := scannerStore.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("reload repository: %v", err)
	}
	if !strings.Contains(reloaded.LastError, "repository_target_not_empty") {
		t.Fatalf("last_error = %q, want repository_target_not_empty", reloaded.LastError)
	}
}

func countHeldOperationReservations(t *testing.T, ctx context.Context, store *Store) int {
	t.Helper()
	var count int
	if err := store.handle.DB.QueryRowContext(ctx, `SELECT count(*) FROM repository_operation_reservations WHERE status = 'held'`).Scan(&count); err != nil {
		t.Fatalf("count held operation reservations: %v", err)
	}
	return count
}

func createGitFixtureRepository(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	runGitCommand(t, "", "init", source)
	mustWriteRepositoryTestFile(t, filepath.Join(source, "README.md"), "fixture\n")
	runGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "add", "README.md")
	runGitCommand(t, source, "-c", "user.name=Test User", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	return source
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

func requireRepositoryPostgresTestDatabase(t *testing.T, dsn string) {
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
	t.Fatalf("refusing destructive repository contract test against database %q; use a test database or set THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS=1", dbName)
}

func resetRepositoryPostgresTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		"DROP TABLE IF EXISTS repository_operation_reservations CASCADE",
		"DROP TABLE IF EXISTS repository_credentials CASCADE",
		"DROP TABLE IF EXISTS repository_provider_instances CASCADE",
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
