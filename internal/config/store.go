package config

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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
	AppliedKeys         []string `json:"applied_keys"`
	RestartRequiredKeys []string `json:"restart_required_keys"`
	FailedKeys          []string `json:"failed_keys"`
	SchemaVersion       string   `json:"schema_version"`
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()

	for _, entry := range entries {
		if err := s.upsertConfigEntry(ctx, tx, entry, now, updatedBy); err != nil {
			return ImportResult{}, err
		}
	}
	if err := s.upsertStorageProfile(ctx, tx, provider, engineFlavor, payload, fingerprint, now); err != nil {
		return ImportResult{}, err
	}
	if err := s.upsertStorageProviderSettings(ctx, tx, provider, now); err != nil {
		return ImportResult{}, err
	}
	if err := s.replaceIgnoreRules(ctx, tx, ignore, now); err != nil {
		return ImportResult{}, err
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

func (s *Store) Reload(ctx context.Context, keys []string) (ReloadResult, error) {
	if len(keys) == 0 {
		loaded, err := s.configKeys(ctx)
		if err != nil {
			return ReloadResult{}, err
		}
		keys = loaded
	}
	return ReloadResult{
		AppliedKeys:         reloadableKeys(keys),
		RestartRequiredKeys: restartRequiredKeys(keys),
		FailedKeys:          []string{},
		SchemaVersion:       "config_reload.result.v1",
	}, nil
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

func (s *Store) upsertStorageProfile(ctx context.Context, tx *sql.Tx, provider, engineFlavor, payload, fingerprint, now string) error {
	slot := "current"
	status := "active"
	id := stableID("storage-profile", slot)
	if s.handle.Provider == "postgres" {
		_, err := tx.ExecContext(ctx, `INSERT INTO storage_profiles (id, slot, provider, engine_flavor, status, config_payload, database_fingerprint, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
ON CONFLICT (id) DO UPDATE SET provider = EXCLUDED.provider, engine_flavor = EXCLUDED.engine_flavor, status = EXCLUDED.status, config_payload = EXCLUDED.config_payload, database_fingerprint = EXCLUDED.database_fingerprint, updated_at = EXCLUDED.updated_at`,
			id, slot, provider, nullEmpty(engineFlavor), status, payload, fingerprint, now)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO storage_profiles (id, slot, provider, engine_flavor, status, config_payload, database_fingerprint, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET provider = excluded.provider, engine_flavor = excluded.engine_flavor, status = excluded.status, config_payload = excluded.config_payload, database_fingerprint = excluded.database_fingerprint, updated_at = excluded.updated_at`,
		id, slot, provider, nullEmpty(engineFlavor), status, payload, fingerprint, now, now)
	return err
}

func (s *Store) upsertStorageProviderSettings(ctx context.Context, tx *sql.Tx, provider, now string) error {
	profileID := stableID("storage-profile", "current")
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

func filterKeys(keys []string, allowed map[string]struct{}) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := allowed[key]; ok {
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
