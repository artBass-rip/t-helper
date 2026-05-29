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
	if value != "stage-05" {
		t.Fatalf("schema_version = %q, want stage-05", value)
	}
}

func TestStage04ReadPathIndexesExistForSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Apply(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	for table, names := range map[string][]string{
		"jobs": {
			"jobs_claim_idx",
			"jobs_worker_status_idx",
			"jobs_worker_status_page_idx",
			"jobs_leased_by_status_idx",
		},
		"workflow_statuses": {
			"workflow_statuses_updated_at_idx",
			"workflow_statuses_filter_updated_at_idx",
		},
		"job_events": {
			"job_events_created_at_idx",
		},
		"job_locks": {
			"job_locks_cleanup_idx",
		},
		"ignore_rules": {
			"ignore_rules_scope_order_idx",
		},
		"root_paths": {
			"root_paths_enabled_path_idx",
		},
		"projects": {
			"projects_root_path_status_idx",
			"projects_repository_id_idx",
			"projects_status_updated_at_idx",
		},
		"repositories": {
			"repositories_provider_host_full_path_idx",
			"repositories_local_path_idx",
			"repositories_root_target_directory_idx",
			"repositories_root_local_path_idx",
			"repositories_status_idx",
			"repositories_discovery_source_idx",
		},
		"project_links": {
			"project_links_repository_id_idx",
		},
		"workspaces": {
			"workspaces_project_id_idx",
			"workspaces_environment_id_idx",
		},
		"repository_provider_instances": {
			"repository_provider_instances_provider_host_idx",
		},
		"repository_credentials": {
			"repository_credentials_provider_instance_idx",
			"repository_credentials_provider_instance_auth_type_idx",
		},
	} {
		got := sqliteIndexes(t, db, table)
		for _, name := range names {
			if !got[name] {
				t.Fatalf("missing index %s on %s; got indexes %#v", name, table, got)
			}
		}
	}
}

func TestStage05RepositoryForeignKeysExistForSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Apply(context.Background(), db, "sqlite"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	got := sqliteForeignKeys(t, db, "repositories")
	for _, want := range []string{
		"provider_instance_id->repository_provider_instances(id)",
		"default_credential_id->repository_credentials(id)",
		"superseded_by_repository_id->repositories(id)",
		"root_path_id->root_paths(id)",
	} {
		if !got[want] {
			t.Fatalf("missing repository foreign key %s; got %#v", want, got)
		}
	}
}

func sqliteIndexes(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA index_list(" + table + ")")
	if err != nil {
		t.Fatalf("list indexes for %s: %v", table, err)
	}
	defer rows.Close()
	indexes := map[string]bool{}
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index for %s: %v", table, err)
		}
		indexes[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate indexes for %s: %v", table, err)
	}
	return indexes
}

func sqliteForeignKeys(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA foreign_key_list(" + table + ")")
	if err != nil {
		t.Fatalf("list foreign keys for %s: %v", table, err)
	}
	defer rows.Close()
	keys := map[string]bool{}
	for rows.Next() {
		var id, seq int
		var refTable, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key for %s: %v", table, err)
		}
		keys[from+"->"+refTable+"("+to+")"] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys for %s: %v", table, err)
	}
	return keys
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
