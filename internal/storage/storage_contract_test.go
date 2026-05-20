package storage_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestStorageContractSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "thelper-stage01.db")
	runStorageContract(t, storage.Config{Provider: "sqlite", DSN: dbPath})
}

func TestStorageContractPostgres(t *testing.T) {
	dsn := os.Getenv("THELPER_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("THELPER_POSTGRES_DSN is not set")
	}
	requirePostgresTestDatabase(t, dsn)
	runStorageContract(t, storage.Config{Provider: "postgres", DSN: dsn})
}

func runStorageContract(t *testing.T, cfg storage.Config) {
	t.Helper()
	ctx := context.Background()
	registry := storageproviders.MVPRegistry()
	handle, err := registry.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer handle.Close()

	resetStage01Tables(t, handle.DB, handle.Provider)
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	if err := handle.DB.PingContext(ctx); err != nil {
		t.Fatalf("ping storage: %v", err)
	}
	assertSystemMetadata(t, handle.DB)
	assertStage02TablesPresent(t, handle.DB, handle.Provider)
	assertStage03TablesPresent(t, handle.DB, handle.Provider)
	assertStage04TablesPresent(t, handle.DB, handle.Provider)
	assertPostStage04TablesAbsent(t, handle.DB, handle.Provider)
	if handle.Fingerprint == "" {
		t.Fatal("expected non-empty database fingerprint")
	}
}

func requirePostgresTestDatabase(t *testing.T, dsn string) {
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
	t.Fatalf("refusing destructive storage contract test against database %q; use a test database or set THELPER_ALLOW_DESTRUCTIVE_STORAGE_TESTS=1", dbName)
}

func resetStage01Tables(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	stage04Drops := []string{
		"DROP TABLE IF EXISTS project_links",
		"DROP TABLE IF EXISTS projects",
		"DROP TABLE IF EXISTS workspaces",
		"DROP TABLE IF EXISTS repositories",
		"DROP TABLE IF EXISTS environments",
		"DROP TABLE IF EXISTS root_paths",
	}
	if provider == "postgres" {
		stage04Drops = []string{
			"DROP TABLE IF EXISTS project_links CASCADE",
			"DROP TABLE IF EXISTS projects CASCADE",
			"DROP TABLE IF EXISTS workspaces CASCADE",
			"DROP TABLE IF EXISTS repositories CASCADE",
			"DROP TABLE IF EXISTS environments CASCADE",
			"DROP TABLE IF EXISTS root_paths CASCADE",
		}
	}
	for _, stmt := range append(stage04Drops, []string{
		"DROP TABLE IF EXISTS workflow_statuses",
		"DROP TABLE IF EXISTS job_events",
		"DROP TABLE IF EXISTS job_locks",
		"DROP TABLE IF EXISTS jobs",
		"DROP TABLE IF EXISTS ignore_rules",
		"DROP TABLE IF EXISTS module_states",
		"DROP TABLE IF EXISTS storage_provider_settings",
		"DROP TABLE IF EXISTS storage_profiles",
		"DROP TABLE IF EXISTS config_entries",
		"DROP TABLE IF EXISTS system_metadata",
		"DROP TABLE IF EXISTS goose_db_version",
	}...) {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("reset table with %q: %v", stmt, err)
		}
	}
}

func assertSystemMetadata(t *testing.T, db *sql.DB) {
	t.Helper()
	var value string
	if err := db.QueryRow("SELECT value FROM system_metadata WHERE key = 'health_schema_version'").Scan(&value); err != nil {
		t.Fatalf("read system metadata: %v", err)
	}
	if value != "health_status.v1" {
		t.Fatalf("unexpected health schema version %q", value)
	}
}

func assertStage02TablesPresent(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	for _, table := range []string{"config_entries", "storage_profiles", "storage_provider_settings", "module_states", "ignore_rules"} {
		if !tableExists(t, db, provider, table) {
			t.Fatalf("Stage 02 table %q was not created", table)
		}
	}
}

func assertStage03TablesPresent(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	for _, table := range []string{"jobs", "job_locks", "job_events", "workflow_statuses"} {
		if !tableExists(t, db, provider, table) {
			t.Fatalf("Stage 03 table %q was not created", table)
		}
	}
}

func assertStage04TablesPresent(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	for _, table := range []string{"root_paths", "projects", "project_links", "repositories", "environments", "workspaces"} {
		if !tableExists(t, db, provider, table) {
			t.Fatalf("Stage 04 table %q was not created", table)
		}
	}
}

func assertPostStage04TablesAbsent(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	laterStageTables := []string{
		"users",
		"project_scans",
		"security_findings",
	}
	for _, table := range laterStageTables {
		if tableExists(t, db, provider, table) {
			t.Fatalf("post-Stage 04 table %q must not be created by Stage 04 migrations", table)
		}
	}
}

func tableExists(t *testing.T, db *sql.DB, provider, table string) bool {
	t.Helper()
	var count int
	var err error
	switch provider {
	case "postgres":
		err = db.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1", table).Scan(&count)
	default:
		err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count)
	}
	if err != nil {
		t.Fatalf("check table %q: %v", table, err)
	}
	return count > 0
}
