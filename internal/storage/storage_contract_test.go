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

	resetStage01Tables(t, handle.DB)
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	if err := handle.DB.PingContext(ctx); err != nil {
		t.Fatalf("ping storage: %v", err)
	}
	assertSystemMetadata(t, handle.DB)
	assertLaterStageTablesAbsent(t, handle.DB, handle.Provider)
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

func resetStage01Tables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		"DROP TABLE IF EXISTS system_metadata",
		"DROP TABLE IF EXISTS goose_db_version",
	} {
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

func assertLaterStageTablesAbsent(t *testing.T, db *sql.DB, provider string) {
	t.Helper()
	laterStageTables := []string{
		"config_entries",
		"module_states",
		"jobs",
		"job_locks",
		"root_paths",
		"projects",
		"repositories",
		"users",
	}
	for _, table := range laterStageTables {
		if tableExists(t, db, provider, table) {
			t.Fatalf("later-stage table %q must not be created by Stage 01 migrations", table)
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
