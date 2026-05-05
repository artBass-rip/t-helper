package migrations

import (
	"context"
	"database/sql"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLogicalMigrationVersionsAreSynchronized(t *testing.T) {
	sqliteVersions := migrationVersions(t, "sqlite")
	postgresVersions := migrationVersions(t, "postgres")
	if len(sqliteVersions) == 0 {
		t.Fatal("expected sqlite migrations")
	}
	if len(postgresVersions) == 0 {
		t.Fatal("expected postgres migrations")
	}
	if len(sqliteVersions) != len(postgresVersions) {
		t.Fatalf("sqlite versions %v, postgres versions %v", sqliteVersions, postgresVersions)
	}
	for i := range sqliteVersions {
		if sqliteVersions[i] != postgresVersions[i] {
			t.Fatalf("sqlite versions %v, postgres versions %v", sqliteVersions, postgresVersions)
		}
	}
}

func TestApplyIsIdempotentForSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Apply(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := Apply(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	var value string
	if err := db.QueryRow("SELECT value FROM system_metadata WHERE key = 'schema_version'").Scan(&value); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if value != "stage-01" {
		t.Fatalf("schema_version = %q, want stage-01", value)
	}
}

func migrationVersions(t *testing.T, dialect string) []string {
	t.Helper()
	entries, err := fs.ReadDir(files, dialect)
	if err != nil {
		t.Fatalf("read migrations for %s: %v", dialect, err)
	}
	versionPattern := regexp.MustCompile(`^([0-9]{6})_.+\.sql$`)
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := versionPattern.FindStringSubmatch(path.Base(entry.Name()))
		if matches == nil {
			t.Fatalf("migration %s/%s does not follow six-digit naming contract", dialect, entry.Name())
		}
		versions = append(versions, matches[1])
	}
	sort.Strings(versions)
	return versions
}
