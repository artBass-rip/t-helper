package config

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/storage"
)

type Store struct {
	handle *storage.Handle
}

type IgnoreRule struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id,omitempty"`
	Pattern   string `json:"pattern"`
	Origin    string `json:"origin"`
}

type ImportResult struct {
	AppliedKeys         []string     `json:"applied_keys"`
	RestartRequiredKeys []string     `json:"restart_required_keys"`
	IgnoreRules         []IgnoreRule `json:"ignore_rules"`
	SchemaVersion       string       `json:"schema_version"`
}

type ReloadResult struct {
	AcceptedKeys        []string `json:"accepted_keys"`
	AppliedKeys         []string `json:"applied_keys"`
	RestartRequiredKeys []string `json:"restart_required_keys"`
	FailedKeys          []string `json:"failed_keys"`
	SchemaVersion       string   `json:"schema_version"`
}

type StorageProfileRecord struct {
	ID                        string
	Slot                      string
	Provider                  string
	EngineFlavor              string
	Status                    string
	ConfigPayload             string
	DatabaseFingerprint       string
	LastMigratedFromProfileID string
	CreatedAt                 string
	UpdatedAt                 string
}

type MigrationResult struct {
	Status                  string `json:"status"`
	PreviousCurrentProfile  string `json:"previous_current_profile_id,omitempty"`
	NewCurrentProfile       string `json:"new_current_profile_id,omitempty"`
	CurrentProfileUnchanged bool   `json:"current_profile_unchanged"`
	SchemaVersion           string `json:"schema_version"`
}

type RuntimeSettings struct {
	ListenAddress  string
	Mode           string
	LogLevel       string
	EnabledModules []string
	Loaded         bool
}

func NewStore(handle *storage.Handle) *Store {
	return &Store{handle: handle}
}

func (s *Store) Import(ctx context.Context, cfg RuntimeConfig, ignore []string, updatedBy string) (ImportResult, error) {
	if err := Validate(cfg); err != nil {
		return ImportResult{}, err
	}
	entries, err := Flatten(cfg)
	if err != nil {
		return ImportResult{}, err
	}
	provider, engineFlavor, payload, fingerprint, err := StorageProfile(cfg)
	if err != nil {
		return ImportResult{}, err
	}
	currentProfile, currentErr := s.CurrentStorageProfile(ctx)
	if currentErr != nil && currentErr != sql.ErrNoRows {
		return ImportResult{}, currentErr
	}
	storageChanged := currentErr == nil && currentProfile.DatabaseFingerprint != fingerprint
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()

	for _, entry := range entries {
		if storageChanged && isStorageKey(entry.Key) {
			continue
		}
		if err := s.upsertConfigEntry(ctx, tx, entry, now, updatedBy); err != nil {
			return ImportResult{}, err
		}
	}
	slot := "current"
	status := "active"
	if storageChanged {
		slot = "migration"
		status = "migration_target"
	}
	if err := s.upsertStorageProfile(ctx, tx, slot, provider, engineFlavor, payload, fingerprint, status, "", now); err != nil {
		return ImportResult{}, err
	}
	if err := s.upsertStorageProviderSettings(ctx, tx, slot, provider, now); err != nil {
		return ImportResult{}, err
	}
	if ignore != nil {
		if err := s.replaceIgnoreRules(ctx, tx, ignore, now); err != nil {
			return ImportResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return ImportResult{
		AppliedKeys:         reloadableKeys(keys),
		RestartRequiredKeys: restartRequiredKeys(keys),
		IgnoreRules:         ignoreRules(ignore),
		SchemaVersion:       "config_import.result.v1",
	}, nil
}

func (s *Store) CurrentStorageProfile(ctx context.Context) (StorageProfileRecord, error) {
	return s.storageProfile(ctx, "current", "active")
}

func (s *Store) MigrationStorageProfile(ctx context.Context) (StorageProfileRecord, error) {
	return s.storageProfile(ctx, "migration", "migration_target")
}

func (s *Store) storageProfile(ctx context.Context, slot, status string) (StorageProfileRecord, error) {
	query := `SELECT id, slot, provider, COALESCE(engine_flavor, ''), status, config_payload, database_fingerprint, COALESCE(last_migrated_from_profile_id, ''), created_at, updated_at FROM storage_profiles WHERE slot = ? AND status = ?`
	args := []any{slot, status}
	if s.handle.Provider == "postgres" {
		query = `SELECT id, slot, provider, COALESCE(engine_flavor, ''), status, config_payload, database_fingerprint, COALESCE(last_migrated_from_profile_id, ''), created_at, updated_at FROM storage_profiles WHERE slot = $1 AND status = $2`
	}
	var profile StorageProfileRecord
	err := s.handle.DB.QueryRowContext(ctx, query, args...).Scan(
		&profile.ID,
		&profile.Slot,
		&profile.Provider,
		&profile.EngineFlavor,
		&profile.Status,
		&profile.ConfigPayload,
		&profile.DatabaseFingerprint,
		&profile.LastMigratedFromProfileID,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	return profile, err
}

func (p StorageProfileRecord) StorageConfig() (storage.Config, error) {
	switch normalizeProviderName(p.Provider) {
	case "sqlite":
		var cfg Database
		if err := json.Unmarshal([]byte(p.ConfigPayload), &cfg); err != nil {
			return storage.Config{}, err
		}
		return storage.Config{Provider: "sqlite", DSN: cfg.DatabasePath}, nil
	case "postgres":
		var cfg ExternalDatabase
		if err := json.Unmarshal([]byte(p.ConfigPayload), &cfg); err != nil {
			return storage.Config{}, err
		}
		username, err := resolveEnvRef(cfg.Username)
		if err != nil {
			return storage.Config{}, err
		}
		password, err := resolveEnvRef(cfg.Password)
		if err != nil {
			return storage.Config{}, err
		}
		dsnURL := url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(username, password),
			Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
			Path:     "/" + cfg.DatabaseName,
			RawQuery: "sslmode=disable",
		}
		dsn := dsnURL.String()
		return storage.Config{Provider: "postgres", DSN: dsn}, nil
	default:
		return storage.Config{}, fmt.Errorf("storage migration provider %q is not supported by this build", p.Provider)
	}
}

func (s *Store) ActiveConfig(ctx context.Context) (map[string]any, error) {
	query := "SELECT key, value, value_type FROM config_entries WHERE scope = ? ORDER BY key"
	if s.handle.Provider == "postgres" {
		query = "SELECT key, value, value_type FROM config_entries WHERE scope = $1 ORDER BY key"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, "system")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var key, value, valueType string
		if err := rows.Scan(&key, &value, &valueType); err != nil {
			return nil, err
		}
		setNested(out, strings.Split(key, "."), typedValue(key, value, valueType))
	}
	return out, rows.Err()
}

func (s *Store) RuntimeSettings(ctx context.Context) (RuntimeSettings, error) {
	query := "SELECT key, value, value_type FROM config_entries WHERE scope = ? AND key IN (?, ?, ?, ?)"
	args := []any{"system", "api.listen_address", "system_settings.mode", "logging.level", "modules.enabled"}
	if s.handle.Provider == "postgres" {
		query = "SELECT key, value, value_type FROM config_entries WHERE scope = $1 AND key IN ($2, $3, $4, $5)"
	}
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer rows.Close()
	settings := RuntimeSettings{}
	for rows.Next() {
		var key, value, valueType string
		if err := rows.Scan(&key, &value, &valueType); err != nil {
			return RuntimeSettings{}, err
		}
		settings.Loaded = true
		switch key {
		case "api.listen_address":
			settings.ListenAddress = value
		case "system_settings.mode":
			settings.Mode = value
		case "logging.level":
			settings.LogLevel = value
		case "modules.enabled":
			if err := json.Unmarshal([]byte(value), &settings.EnabledModules); err != nil {
				return RuntimeSettings{}, err
			}
		}
		_ = valueType
	}
	return settings, rows.Err()
}

func (s *Store) Reload(ctx context.Context, keys []string) (ReloadResult, error) {
	explicitKeys := len(keys) > 0
	if len(keys) == 0 {
		loaded, err := s.configKeys(ctx)
		if err != nil {
			return ReloadResult{}, err
		}
		keys = loaded
	}
	if explicitKeys {
		if failed := unknownReloadKeys(keys); len(failed) > 0 {
			return ReloadResult{
				AcceptedKeys:        []string{},
				AppliedKeys:         []string{},
				RestartRequiredKeys: restartRequiredKeys(keys),
				FailedKeys:          failed,
				SchemaVersion:       "config_reload.result.v1",
			}, nil
		}
	}
	accepted := reloadableKeys(keys)
	return ReloadResult{
		AcceptedKeys:        accepted,
		AppliedKeys:         []string{},
		RestartRequiredKeys: restartRequiredKeys(keys),
		FailedKeys:          []string{},
		SchemaVersion:       "config_reload.result.v1",
	}, nil
}

func (s *Store) MigrateDB(ctx context.Context, registry *storage.Registry) (MigrationResult, error) {
	current, err := s.CurrentStorageProfile(ctx)
	if err != nil {
		return MigrationResult{}, err
	}
	migration, err := s.MigrationStorageProfile(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return MigrationResult{
				Status:                  "no_migration_target",
				CurrentProfileUnchanged: true,
				SchemaVersion:           "storage_migration.result.v1",
			}, nil
		}
		return MigrationResult{}, err
	}
	targetCfg, err := migration.StorageConfig()
	if err != nil {
		return MigrationResult{}, err
	}
	target, err := registry.Open(ctx, targetCfg)
	if err != nil {
		return MigrationResult{}, err
	}
	defer target.Close()
	if err := registry.Migrate(ctx, target); err != nil {
		_ = s.markMigrationFailed(ctx, migration.ID)
		return MigrationResult{}, err
	}
	if target.Fingerprint != migration.DatabaseFingerprint {
		_ = s.markMigrationFailed(ctx, migration.ID)
		return MigrationResult{}, fmt.Errorf("migration target fingerprint mismatch")
	}
	if err := s.copyStage02Data(ctx, target, current, migration); err != nil {
		_ = s.markMigrationFailed(ctx, migration.ID)
		return MigrationResult{}, err
	}
	if err := s.promoteMigrationProfile(ctx, current, migration); err != nil {
		return MigrationResult{}, err
	}
	return MigrationResult{
		Status:                  "migration_succeeded",
		PreviousCurrentProfile:  current.ID,
		NewCurrentProfile:       migration.ID,
		CurrentProfileUnchanged: false,
		SchemaVersion:           "storage_migration.result.v1",
	}, nil
}

func (s *Store) copyStage02Data(ctx context.Context, target *storage.Handle, current, migration StorageProfileRecord) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := target.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := clearStage02Tables(ctx, tx); err != nil {
		return err
	}
	if err := s.copyConfigEntries(ctx, tx, target.Provider, migration, now); err != nil {
		return err
	}
	if err := s.copyIgnoreRules(ctx, tx, target.Provider); err != nil {
		return err
	}
	if err := s.copyModuleStates(ctx, tx, target.Provider); err != nil {
		return err
	}
	if err := insertHistoricalProfile(ctx, tx, target.Provider, current, now); err != nil {
		return err
	}
	if err := insertProfile(ctx, tx, target.Provider, migration.ID, "current", migration.Provider, migration.EngineFlavor, "active", migration.ConfigPayload, migration.DatabaseFingerprint, current.ID, now); err != nil {
		return err
	}
	if err := (&Store{handle: target}).upsertStorageProviderSettingsForProfile(ctx, tx, migration.ID, migration.Provider, now); err != nil {
		return err
	}
	return tx.Commit()
}

func clearStage02Tables(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		"DELETE FROM ignore_rules",
		"DELETE FROM module_states",
		"DELETE FROM storage_provider_settings",
		"DELETE FROM storage_profiles",
		"DELETE FROM config_entries",
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) copyConfigEntries(ctx context.Context, tx *sql.Tx, targetProvider string, migration StorageProfileRecord, now string) error {
	rows, err := s.handle.DB.QueryContext(ctx, "SELECT key, value, value_type, scope, version, updated_at, updated_by FROM config_entries ORDER BY key, scope")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var entry Entry
		var scope, updatedAt, updatedBy string
		var version int
		if err := rows.Scan(&entry.Key, &entry.Value, &entry.ValueType, &scope, &version, &updatedAt, &updatedBy); err != nil {
			return err
		}
		if isStorageKey(entry.Key) {
			continue
		}
		if err := insertConfigEntry(ctx, tx, targetProvider, entry, scope, version, updatedAt, updatedBy); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	storageEntries, err := storageEntriesFromProfile(migration)
	if err != nil {
		return err
	}
	for _, entry := range storageEntries {
		if err := insertConfigEntry(ctx, tx, targetProvider, entry, "system", 1, now, "storage_migration"); err != nil {
			return err
		}
	}
	return nil
}

func insertConfigEntry(ctx context.Context, tx *sql.Tx, provider string, entry Entry, scope string, version int, updatedAt, updatedBy string) error {
	id := stableID("config", entry.Key, scope)
	if provider == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO config_entries (id, key, value, value_type, scope, version, updated_at, updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			id, entry.Key, entry.Value, entry.ValueType, scope, version, updatedAt, updatedBy)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO config_entries (id, key, value, value_type, scope, version, updated_at, updated_by) VALUES (?,?,?,?,?,?,?,?)`,
		id, entry.Key, entry.Value, entry.ValueType, scope, version, updatedAt, updatedBy)
	return err
}

func (s *Store) copyIgnoreRules(ctx context.Context, tx *sql.Tx, targetProvider string) error {
	rows, err := s.handle.DB.QueryContext(ctx, "SELECT id, scope_type, COALESCE(scope_id, ''), pattern, origin, created_at, updated_at FROM ignore_rules ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, scopeType, scopeID, pattern, origin, createdAt, updatedAt string
		if err := rows.Scan(&id, &scopeType, &scopeID, &pattern, &origin, &createdAt, &updatedAt); err != nil {
			return err
		}
		if targetProvider == "postgres" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO ignore_rules (id, scope_type, scope_id, pattern, origin, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, scopeType, scopeID, pattern, origin, createdAt, updatedAt); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ignore_rules (id, scope_type, scope_id, pattern, origin, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`, id, scopeType, scopeID, pattern, origin, createdAt, updatedAt); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) copyModuleStates(ctx context.Context, tx *sql.Tx, targetProvider string) error {
	rows, err := s.handle.DB.QueryContext(ctx, "SELECT id, module_name, state, pid, host, details, updated_at FROM module_states ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, moduleName, state, details, updatedAt string
		var pid sql.NullInt64
		var host sql.NullString
		if err := rows.Scan(&id, &moduleName, &state, &pid, &host, &details, &updatedAt); err != nil {
			return err
		}
		if targetProvider == "postgres" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO module_states (id, module_name, state, pid, host, details, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, moduleName, state, nullableInt(pid), nullableString(host), details, updatedAt); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO module_states (id, module_name, state, pid, host, details, updated_at) VALUES (?,?,?,?,?,?,?)`, id, moduleName, state, nullableInt(pid), nullableString(host), details, updatedAt); err != nil {
			return err
		}
	}
	return rows.Err()
}

func insertHistoricalProfile(ctx context.Context, tx *sql.Tx, targetProvider string, current StorageProfileRecord, now string) error {
	return insertProfile(ctx, tx, targetProvider, stableID("storage-profile", "historical", current.ID, now), "historical", current.Provider, current.EngineFlavor, "superseded", current.ConfigPayload, current.DatabaseFingerprint, "", now)
}

func insertProfile(ctx context.Context, tx *sql.Tx, targetProvider, id, slot, provider, engineFlavor, status, payload, fingerprint, lastMigratedFromProfileID, now string) error {
	if targetProvider == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO storage_profiles (id, slot, provider, engine_flavor, status, config_payload, database_fingerprint, last_migrated_from_profile_id, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`,
			id, slot, provider, nullEmpty(engineFlavor), status, payload, fingerprint, nullEmpty(lastMigratedFromProfileID), now)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO storage_profiles (id, slot, provider, engine_flavor, status, config_payload, database_fingerprint, last_migrated_from_profile_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, slot, provider, nullEmpty(engineFlavor), status, payload, fingerprint, nullEmpty(lastMigratedFromProfileID), now, now)
	return err
}

func (s *Store) markMigrationFailed(ctx context.Context, migrationID string) error {
	query := "UPDATE storage_profiles SET status = ?, updated_at = ? WHERE id = ?"
	args := []any{"migration_failed", time.Now().UTC().Format(time.RFC3339Nano), migrationID}
	if s.handle.Provider == "postgres" {
		query = "UPDATE storage_profiles SET status = $1, updated_at = $2 WHERE id = $3"
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) promoteMigrationProfile(ctx context.Context, current, migration StorageProfileRecord) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.handle.Provider == "postgres" {
		if _, err := tx.ExecContext(ctx, `UPDATE storage_profiles SET slot = 'historical', status = 'superseded', updated_at = $1 WHERE id = $2`, now, current.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE storage_profiles SET slot = 'current', status = 'active', last_migrated_from_profile_id = $1, updated_at = $2 WHERE id = $3`, current.ID, now, migration.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE storage_profiles SET slot = 'historical', status = 'superseded', updated_at = ? WHERE id = ?`, now, current.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE storage_profiles SET slot = 'current', status = 'active', last_migrated_from_profile_id = ?, updated_at = ? WHERE id = ?`, current.ID, now, migration.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func nullableInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func LoadIgnoreFile(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func (s *Store) configKeys(ctx context.Context) ([]string, error) {
	query, args := s.placeholder("SELECT key FROM config_entries WHERE scope = %s ORDER BY key", "system")
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) upsertConfigEntry(ctx context.Context, tx *sql.Tx, entry Entry, now, updatedBy string) error {
	id := stableID("config", entry.Key, "system")
	if s.handle.Provider == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO config_entries (id, key, value, value_type, scope, version, updated_at, updated_by)
VALUES ($1, $2, $3, $4, 'system', 1, $5, $6)
ON CONFLICT (key, scope) DO UPDATE SET value = EXCLUDED.value, value_type = EXCLUDED.value_type, version = config_entries.version + 1, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
			id, entry.Key, entry.Value, entry.ValueType, now, updatedBy)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO config_entries (id, key, value, value_type, scope, version, updated_at, updated_by)
VALUES (?, ?, ?, ?, 'system', 1, ?, ?)
ON CONFLICT (key, scope) DO UPDATE SET value = excluded.value, value_type = excluded.value_type, version = config_entries.version + 1, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		id, entry.Key, entry.Value, entry.ValueType, now, updatedBy)
	return err
}

func (s *Store) upsertStorageProfile(ctx context.Context, tx *sql.Tx, slot, provider, engineFlavor, payload, fingerprint, status, lastMigratedFromProfileID, now string) error {
	id := stableID("storage-profile", slot)
	if s.handle.Provider == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO storage_profiles (id, slot, provider, engine_flavor, status, config_payload, database_fingerprint, last_migrated_from_profile_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
ON CONFLICT (id) DO UPDATE SET provider = EXCLUDED.provider, engine_flavor = EXCLUDED.engine_flavor, status = EXCLUDED.status, config_payload = EXCLUDED.config_payload, database_fingerprint = EXCLUDED.database_fingerprint, last_migrated_from_profile_id = EXCLUDED.last_migrated_from_profile_id, updated_at = EXCLUDED.updated_at`,
			id, slot, provider, nullEmpty(engineFlavor), status, payload, fingerprint, nullEmpty(lastMigratedFromProfileID), now)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO storage_profiles (id, slot, provider, engine_flavor, status, config_payload, database_fingerprint, last_migrated_from_profile_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET provider = excluded.provider, engine_flavor = excluded.engine_flavor, status = excluded.status, config_payload = excluded.config_payload, database_fingerprint = excluded.database_fingerprint, last_migrated_from_profile_id = excluded.last_migrated_from_profile_id, updated_at = excluded.updated_at`,
		id, slot, provider, nullEmpty(engineFlavor), status, payload, fingerprint, nullEmpty(lastMigratedFromProfileID), now, now)
	return err
}

func (s *Store) upsertStorageProviderSettings(ctx context.Context, tx *sql.Tx, slot, provider, now string) error {
	profileID := stableID("storage-profile", slot)
	return s.upsertStorageProviderSettingsForProfile(ctx, tx, profileID, provider, now)
}

func (s *Store) upsertStorageProviderSettingsForProfile(ctx context.Context, tx *sql.Tx, profileID, provider, now string) error {
	id := stableID("storage-provider-settings", profileID, provider)
	if provider == "sqlite" {
		return s.upsertProviderSettingsRow(ctx, tx, id, profileID, provider, 1, 1, "5s", "30s", "10s", "WAL", true, now)
	}
	return s.upsertProviderSettingsRow(ctx, tx, id, profileID, provider, 4, 4, "5s", "30s", "10s", "", false, now)
}

func (s *Store) upsertProviderSettingsRow(ctx context.Context, tx *sql.Tx, id, profileID, provider string, concurrency, processLimit int, busyTimeout, leaseDuration, heartbeatInterval, journalMode string, foreignKeys bool, now string) error {
	if s.handle.Provider == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO storage_provider_settings (id, storage_profile_id, provider, workers_concurrency, worker_process_limit, busy_timeout, lease_duration, heartbeat_interval, sqlite_journal_mode, sqlite_foreign_keys, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
ON CONFLICT (id) DO UPDATE SET workers_concurrency = EXCLUDED.workers_concurrency, worker_process_limit = EXCLUDED.worker_process_limit, busy_timeout = EXCLUDED.busy_timeout, lease_duration = EXCLUDED.lease_duration, heartbeat_interval = EXCLUDED.heartbeat_interval, sqlite_journal_mode = EXCLUDED.sqlite_journal_mode, sqlite_foreign_keys = EXCLUDED.sqlite_foreign_keys, updated_at = EXCLUDED.updated_at`,
			id, profileID, provider, concurrency, processLimit, busyTimeout, leaseDuration, heartbeatInterval, nullEmpty(journalMode), foreignKeys, now)
		return err
	}
	foreignKeysInt := 0
	if foreignKeys {
		foreignKeysInt = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO storage_provider_settings (id, storage_profile_id, provider, workers_concurrency, worker_process_limit, busy_timeout, lease_duration, heartbeat_interval, sqlite_journal_mode, sqlite_foreign_keys, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT (id) DO UPDATE SET workers_concurrency = excluded.workers_concurrency, worker_process_limit = excluded.worker_process_limit, busy_timeout = excluded.busy_timeout, lease_duration = excluded.lease_duration, heartbeat_interval = excluded.heartbeat_interval, sqlite_journal_mode = excluded.sqlite_journal_mode, sqlite_foreign_keys = excluded.sqlite_foreign_keys, updated_at = excluded.updated_at`,
		id, profileID, provider, concurrency, processLimit, busyTimeout, leaseDuration, heartbeatInterval, nullEmpty(journalMode), foreignKeysInt, now, now)
	return err
}

func (s *Store) replaceIgnoreRules(ctx context.Context, tx *sql.Tx, patterns []string, now string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM ignore_rules WHERE scope_type = 'system' AND origin = 'config_import'"); err != nil {
		return err
	}
	for _, pattern := range patterns {
		id := stableID("ignore", "system", pattern)
		if s.handle.Provider == "postgres" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO ignore_rules (id, scope_type, scope_id, pattern, origin, created_at, updated_at) VALUES ($1, 'system', '', $2, 'config_import', $3, $3)`, id, pattern, now); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ignore_rules (id, scope_type, scope_id, pattern, origin, created_at, updated_at) VALUES (?, 'system', '', ?, 'config_import', ?, ?)`, id, pattern, now, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) placeholder(format string, args ...any) (string, []any) {
	if s.handle.Provider != "postgres" {
		return fmt.Sprintf(format, "?"), args
	}
	return fmt.Sprintf(format, "$1"), args
}

func typedValue(key, value, valueType string) any {
	if isSensitiveKey(key) {
		return MaskSensitiveValue(key, value)
	}
	switch valueType {
	case "bool":
		return value == "true"
	case "int":
		var out int
		_, _ = fmt.Sscanf(value, "%d", &out)
		return out
	case "json":
		var out any
		if err := json.Unmarshal([]byte(value), &out); err != nil {
			return nil
		}
		return out
	default:
		return value
	}
}

func setNested(root map[string]any, parts []string, value any) {
	if len(parts) == 1 {
		root[parts[0]] = value
		return
	}
	next, ok := root[parts[0]].(map[string]any)
	if !ok {
		next = map[string]any{}
		root[parts[0]] = next
	}
	setNested(next, parts[1:], value)
}

func reloadableKeys(keys []string) []string {
	reloadable := map[string]struct{}{
		"scanning.global_scan":               {},
		"scanning.security_scan.modules":     {},
		"scanning.toolchain.profile_paths":   {},
		"scanning.toolchain.version_policy":  {},
		"repositories.default_auth_type":     {},
		"repositories.poll_interval_default": {},
		"repositories.auto_sync_default":     {},
		"security.active_rule_set_id":        {},
		"logging.level":                      {},
		"logging.format":                     {},
		"logging.log_path":                   {},
		"modules.enabled":                    {},
	}
	return filterKeys(keys, reloadable)
}

func restartRequiredKeys(keys []string) []string {
	restartRequired := map[string]struct{}{
		"api.listen_address":   {},
		"auth.local_enabled":   {},
		"workers.enabled":      {},
		"workers.concurrency":  {},
		"system_settings.mode": {},
	}
	return filterKeys(keys, restartRequired)
}

func unknownReloadKeys(keys []string) []string {
	known := map[string]struct{}{}
	for _, key := range reloadableKeys(keys) {
		known[key] = struct{}{}
	}
	for _, key := range restartRequiredKeys(keys) {
		known[key] = struct{}{}
	}
	failed := make([]string, 0)
	seen := map[string]struct{}{}
	for _, key := range keys {
		if _, ok := known[key]; ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		failed = append(failed, key)
	}
	sort.Strings(failed)
	return failed
}

func filterKeys(keys []string, allowed map[string]struct{}) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	for _, key := range keys {
		if _, ok := allowed[key]; ok {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	return out
}

func ignoreRules(patterns []string) []IgnoreRule {
	rules := make([]IgnoreRule, 0, len(patterns))
	for _, pattern := range patterns {
		rules = append(rules, IgnoreRule{ScopeType: "system", Pattern: pattern, Origin: "config_import"})
	}
	return rules
}

func stableID(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "stg02_" + hex.EncodeToString(hash[:16])
}

func nullEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func isStorageKey(key string) bool {
	return strings.HasPrefix(key, "database.") || strings.HasPrefix(key, "external_databases.")
}

func storageEntriesFromProfile(profile StorageProfileRecord) ([]Entry, error) {
	switch normalizeProviderName(profile.Provider) {
	case "sqlite":
		var cfg Database
		if err := json.Unmarshal([]byte(profile.ConfigPayload), &cfg); err != nil {
			return nil, err
		}
		external := ExternalDatabase{}
		return []Entry{
			stringEntry("database.database_type", cfg.DatabaseType),
			stringEntry("database.database_path", cfg.DatabasePath),
			boolEntry("external_databases.enabled", false),
			stringEntry("external_databases.provider", external.Provider),
			stringEntry("external_databases.engine_flavor", external.EngineFlavor),
			stringEntry("external_databases.host", external.Host),
			intEntry("external_databases.port", external.Port),
			stringEntry("external_databases.username", external.Username),
			stringEntry("external_databases.password", external.Password),
			stringEntry("external_databases.database_name", external.DatabaseName),
		}, nil
	case "postgres":
		var cfg ExternalDatabase
		if err := json.Unmarshal([]byte(profile.ConfigPayload), &cfg); err != nil {
			return nil, err
		}
		cfg.Enabled = true
		return []Entry{
			stringEntry("database.database_type", "sqlite"),
			stringEntry("database.database_path", ""),
			boolEntry("external_databases.enabled", true),
			stringEntry("external_databases.provider", cfg.Provider),
			stringEntry("external_databases.engine_flavor", cfg.EngineFlavor),
			stringEntry("external_databases.host", cfg.Host),
			intEntry("external_databases.port", cfg.Port),
			stringEntry("external_databases.username", cfg.Username),
			stringEntry("external_databases.password", cfg.Password),
			stringEntry("external_databases.database_name", cfg.DatabaseName),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported storage profile provider %q", profile.Provider)
	}
}

func normalizeProviderName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "postgresql":
		return "postgres"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func resolveEnvRef(ref string) (string, error) {
	const prefix = "secretref://env/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("expected %s secret reference", prefix)
	}
	name := strings.TrimPrefix(ref, prefix)
	value, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment secret %q is not set", name)
	}
	return value, nil
}
