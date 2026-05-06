package config_test

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestStoreImportPersistsConfigAndMasksSecrets(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	file, err := os.Open("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		t.Fatalf("decode example: %v", err)
	}
	store := appconfig.NewStore(handle)
	result, err := store.Import(ctx, cfg, []string{".terraform/", "!keep"}, "test")
	if err != nil {
		t.Fatalf("import config: %v", err)
	}
	if len(result.AppliedKeys) == 0 {
		t.Fatal("expected applied reloadable keys")
	}

	active, err := store.ActiveConfig(ctx)
	if err != nil {
		t.Fatalf("active config: %v", err)
	}
	external := active["external_databases"].(map[string]any)
	password := external["password"].(map[string]any)
	if password["masked"] != true || password["ref_type"] != "env" {
		t.Fatalf("password not masked: %#v", password)
	}

	var rules int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM ignore_rules WHERE origin = 'config_import'").Scan(&rules); err != nil {
		t.Fatal(err)
	}
	if rules != 2 {
		t.Fatalf("ignore rules = %d, want 2", rules)
	}
}

func TestStoreImportNilIgnoreRulesPreservesExistingRules(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	cfg := loadExampleConfig(t)
	store := appconfig.NewStore(handle)
	if _, err := store.Import(ctx, cfg, []string{".terraform/", "!keep"}, "test"); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	next := cfg
	next.Logging.Level = "debug"
	if _, err := store.Import(ctx, next, nil, "api"); err != nil {
		t.Fatalf("config-only import: %v", err)
	}

	var rules int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM ignore_rules WHERE origin = 'config_import'").Scan(&rules); err != nil {
		t.Fatal(err)
	}
	if rules != 2 {
		t.Fatalf("ignore rules = %d, want preserved 2", rules)
	}
}

func TestStoreImportRejectsSensitiveLiteralsWhenExternalDatabaseDisabled(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	cfg := loadExampleConfig(t)
	cfg.ExternalDatabase.Enabled = false
	cfg.ExternalDatabase.Username = "admin"
	cfg.ExternalDatabase.Password = "secret"
	if _, err := appconfig.NewStore(handle).Import(ctx, cfg, nil, "test"); err == nil {
		t.Fatal("expected sensitive literal import to fail")
	}
	var entries int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM config_entries").Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("config entries after rejected import = %d, want 0", entries)
	}
}

func TestStoreImportStorageChangeCreatesMigrationProfileAndMigrateDBPromotesIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	targetPath := filepath.Join(dir, "target.db")
	handle := openMigratedSQLitePath(t, sourcePath)
	defer handle.Close()

	cfg := loadExampleConfig(t)
	cfg.Database.DatabasePath = sourcePath
	store := appconfig.NewStore(handle)
	if _, err := store.Import(ctx, cfg, []string{".terraform/"}, "test"); err != nil {
		t.Fatalf("initial import: %v", err)
	}

	next := cfg
	next.Database.DatabasePath = targetPath
	if _, err := store.Import(ctx, next, []string{".terraform/", "!keep"}, "test"); err != nil {
		t.Fatalf("migration target import: %v", err)
	}

	active, err := store.ActiveConfig(ctx)
	if err != nil {
		t.Fatalf("active config: %v", err)
	}
	database := active["database"].(map[string]any)
	if database["database_path"] != sourcePath {
		t.Fatalf("active database path changed before migrate: %#v", database["database_path"])
	}
	migrationProfile, err := store.MigrationStorageProfile(ctx)
	if err != nil {
		t.Fatalf("migration profile: %v", err)
	}
	if migrationProfile.Status != "migration_target" || migrationProfile.DatabaseFingerprint == "" {
		t.Fatalf("unexpected migration profile: %+v", migrationProfile)
	}

	result, err := store.MigrateDB(ctx, storageproviders.MVPRegistry())
	if err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	if result.Status != "migration_succeeded" || result.CurrentProfileUnchanged {
		t.Fatalf("unexpected migration result: %+v", result)
	}
	current, err := store.CurrentStorageProfile(ctx)
	if err != nil {
		t.Fatalf("current profile after migrate: %v", err)
	}
	if current.DatabaseFingerprint != migrationProfile.DatabaseFingerprint || current.LastMigratedFromProfileID == "" {
		t.Fatalf("migration profile not promoted in source metadata: %+v", current)
	}

	target := openMigratedSQLitePath(t, targetPath)
	defer target.Close()
	targetConfig, err := appconfig.NewStore(target).ActiveConfig(ctx)
	if err != nil {
		t.Fatalf("target active config: %v", err)
	}
	targetDatabase := targetConfig["database"].(map[string]any)
	if targetDatabase["database_path"] != targetPath {
		t.Fatalf("target database path = %#v, want %q", targetDatabase["database_path"], targetPath)
	}
	var ignoreRules int
	if err := target.DB.QueryRowContext(ctx, "SELECT count(*) FROM ignore_rules").Scan(&ignoreRules); err != nil {
		t.Fatal(err)
	}
	if ignoreRules != 2 {
		t.Fatalf("target ignore rules = %d, want 2", ignoreRules)
	}
}

func TestMigrateDBFailureDoesNotChangeCurrentProfile(t *testing.T) {
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	handle := openMigratedSQLitePath(t, sourcePath)
	defer handle.Close()

	cfg := loadExampleConfig(t)
	cfg.Database.DatabasePath = sourcePath
	store := appconfig.NewStore(handle)
	if _, err := store.Import(ctx, cfg, nil, "test"); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	currentBefore, err := store.CurrentStorageProfile(ctx)
	if err != nil {
		t.Fatalf("current before: %v", err)
	}

	next := cfg
	next.ExternalDatabase.Enabled = true
	next.ExternalDatabase.Provider = "mysql"
	next.ExternalDatabase.EngineFlavor = "standard"
	next.ExternalDatabase.Host = "mysql.example.invalid"
	next.ExternalDatabase.Port = 3306
	next.ExternalDatabase.Username = "secretref://env/THELPER_MYSQL_USER"
	next.ExternalDatabase.Password = "secretref://env/THELPER_MYSQL_PASSWORD"
	next.ExternalDatabase.DatabaseName = "t_helper"
	if _, err := store.Import(ctx, next, nil, "test"); err != nil {
		t.Fatalf("migration target import: %v", err)
	}
	if _, err := store.MigrateDB(ctx, storageproviders.MVPRegistry()); err == nil {
		t.Fatal("expected unsupported migration provider error")
	}
	var failed int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM storage_profiles WHERE slot = 'migration' AND status = 'migration_failed'").Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("failed migration profiles = %d, want 1", failed)
	}
	currentAfter, err := store.CurrentStorageProfile(ctx)
	if err != nil {
		t.Fatalf("current after: %v", err)
	}
	if currentAfter.ID != currentBefore.ID || currentAfter.DatabaseFingerprint != currentBefore.DatabaseFingerprint {
		t.Fatalf("current changed after failed migration: before=%+v after=%+v", currentBefore, currentAfter)
	}
}

func TestStorageProfilePostgresDSNEscapesCredentials(t *testing.T) {
	profile := appconfig.StorageProfileRecord{
		Provider: "postgres",
		ConfigPayload: `{
			"enabled": true,
			"provider": "postgresql",
			"engine_flavor": "standard",
			"host": "postgres.example.internal",
			"port": 5432,
			"username": "secretref://env/STAGE02_DSN_USER",
			"password": "secretref://env/STAGE02_DSN_PASSWORD",
			"database_name": "t_helper"
		}`,
	}
	t.Setenv("STAGE02_DSN_USER", "user:name")
	t.Setenv("STAGE02_DSN_PASSWORD", "p@ss/w:rd?#")
	cfg, err := profile.StorageConfig()
	if err != nil {
		t.Fatalf("storage config: %v", err)
	}
	parsed, err := url.Parse(cfg.DSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	password, _ := parsed.User.Password()
	if parsed.User.Username() != "user:name" || password != "p@ss/w:rd?#" {
		t.Fatalf("credentials not preserved after escaping: %s", cfg.DSN)
	}
	if parsed.Host != "postgres.example.internal:5432" || parsed.Path != "/t_helper" || parsed.Query().Get("sslmode") != "disable" {
		t.Fatalf("unexpected postgres dsn: %s", cfg.DSN)
	}
}

func TestStorageProfilePostgresDSNUsesJoinHostPortForIPv6(t *testing.T) {
	profile := appconfig.StorageProfileRecord{
		Provider: "postgres",
		ConfigPayload: `{
			"enabled": true,
			"provider": "postgresql",
			"engine_flavor": "standard",
			"host": "::1",
			"port": 5432,
			"username": "secretref://env/STAGE02_DSN_USER",
			"password": "secretref://env/STAGE02_DSN_PASSWORD",
			"database_name": "t_helper"
		}`,
	}
	t.Setenv("STAGE02_DSN_USER", "user")
	t.Setenv("STAGE02_DSN_PASSWORD", "password")
	cfg, err := profile.StorageConfig()
	if err != nil {
		t.Fatalf("storage config: %v", err)
	}
	parsed, err := url.Parse(cfg.DSN)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if parsed.Host != "[::1]:5432" {
		t.Fatalf("host = %q, want IPv6 join host/port in %s", parsed.Host, cfg.DSN)
	}
}

func TestMigrateDBSQLiteToPostgresWithEnvSecretRefs(t *testing.T) {
	postgresDSN := os.Getenv("THELPER_POSTGRES_DSN")
	if postgresDSN == "" {
		t.Skip("THELPER_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	handle := openMigratedSQLitePath(t, sourcePath)
	defer handle.Close()

	cfg := loadExampleConfig(t)
	cfg.Database.DatabasePath = sourcePath
	store := appconfig.NewStore(handle)
	if _, err := store.Import(ctx, cfg, []string{".terraform/", "!keep"}, "test"); err != nil {
		t.Fatalf("initial import: %v", err)
	}

	pg := parsePostgresDSN(t, postgresDSN)
	t.Setenv("STAGE02_PG_USER", pg.username)
	t.Setenv("STAGE02_PG_PASSWORD", pg.password)
	target := cfg
	target.ExternalDatabase.Enabled = true
	target.ExternalDatabase.Provider = "postgresql"
	target.ExternalDatabase.EngineFlavor = "standard"
	target.ExternalDatabase.Host = pg.host
	target.ExternalDatabase.Port = pg.port
	target.ExternalDatabase.Username = "secretref://env/STAGE02_PG_USER"
	target.ExternalDatabase.Password = "secretref://env/STAGE02_PG_PASSWORD"
	target.ExternalDatabase.DatabaseName = pg.database
	if _, err := store.Import(ctx, target, []string{".terraform/", "!keep", ".cache/"}, "test"); err != nil {
		t.Fatalf("target import: %v", err)
	}
	migration, err := store.MigrationStorageProfile(ctx)
	if err != nil {
		t.Fatalf("migration profile: %v", err)
	}
	if migration.Provider != "postgres" {
		t.Fatalf("migration provider = %q, want postgres", migration.Provider)
	}

	result, err := store.MigrateDB(ctx, storageproviders.MVPRegistry())
	if err != nil {
		t.Fatalf("migrate sqlite to postgres: %v", err)
	}
	if result.Status != "migration_succeeded" || result.CurrentProfileUnchanged {
		t.Fatalf("unexpected migration result: %+v", result)
	}

	pgHandle, err := storageproviders.MVPRegistry().Open(ctx, storage.Config{Provider: "postgres", DSN: postgresDSN})
	if err != nil {
		t.Fatalf("open postgres target: %v", err)
	}
	defer pgHandle.Close()
	if err := storageproviders.MVPRegistry().Migrate(ctx, pgHandle); err != nil {
		t.Fatalf("migrate postgres target: %v", err)
	}
	active, err := appconfig.NewStore(pgHandle).ActiveConfig(ctx)
	if err != nil {
		t.Fatalf("postgres active config: %v", err)
	}
	external := active["external_databases"].(map[string]any)
	if external["enabled"] != true {
		t.Fatalf("postgres external database config not active: %#v", external)
	}
	if password := external["password"].(map[string]any); password["masked"] != true || password["ref_type"] != "env" {
		t.Fatalf("postgres password not masked: %#v", password)
	}
	var rules int
	if err := pgHandle.DB.QueryRowContext(ctx, "SELECT count(*) FROM ignore_rules").Scan(&rules); err != nil {
		t.Fatal(err)
	}
	if rules != 3 {
		t.Fatalf("postgres ignore rules = %d, want 3", rules)
	}
}

type postgresTarget struct {
	host     string
	port     int
	database string
	username string
	password string
}

func parsePostgresDSN(t *testing.T, dsn string) postgresTarget {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	host := parsed.Hostname()
	portText := parsed.Port()
	if portText == "" {
		portText = "5432"
	}
	_, portString, err := net.SplitHostPort(net.JoinHostPort(host, portText))
	if err != nil {
		t.Fatalf("parse postgres host/port: %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portString, "%d", &port); err != nil {
		t.Fatalf("parse postgres port: %v", err)
	}
	password, _ := parsed.User.Password()
	return postgresTarget{
		host:     host,
		port:     port,
		database: strings.TrimPrefix(parsed.Path, "/"),
		username: parsed.User.Username(),
		password: password,
	}
}

func openMigratedSQLite(t *testing.T) *storage.Handle {
	t.Helper()
	return openMigratedSQLitePath(t, filepath.Join(t.TempDir(), "stage02.db"))
}

func openMigratedSQLitePath(t *testing.T, path string) *storage.Handle {
	t.Helper()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(context.Background(), storage.Config{Provider: "sqlite", DSN: path})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := provider.Migrate(context.Background(), handle); err != nil {
		handle.Close()
		t.Fatalf("migrate sqlite: %v", err)
	}
	return handle
}

func loadExampleConfig(t *testing.T) appconfig.RuntimeConfig {
	t.Helper()
	file, err := os.Open("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
