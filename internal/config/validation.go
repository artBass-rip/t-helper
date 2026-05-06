package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var allowedTopLevelKeys = map[string]struct{}{
	"system_settings":    {},
	"database":           {},
	"external_databases": {},
	"scanning":           {},
	"repositories":       {},
	"security":           {},
	"api":                {},
	"auth":               {},
	"workers":            {},
	"modules":            {},
	"logging":            {},
}

var allowedScanningKeys = map[string]struct{}{
	"global_scan":   {},
	"security_scan": {},
	"toolchain":     {},
}

var rejectedGlobalScanAliases = map[string]struct{}{
	"global_scann":      {},
	"globalScan":        {},
	"global_scan_roots": {},
	"scan_roots":        {},
}

func ValidateImportShape(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var root map[string]any
	decoder := json.NewDecoder(r)
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	for key := range root {
		if _, ok := allowedTopLevelKeys[key]; !ok {
			return fmt.Errorf("%s: unknown top-level config key", key)
		}
	}
	if scanningRaw, ok := root["scanning"]; ok {
		scanning, ok := scanningRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("scanning: expected object")
		}
		for key := range scanning {
			if _, ok := rejectedGlobalScanAliases[key]; ok {
				return fmt.Errorf("scanning.%s: unknown key; use scanning.global_scan", key)
			}
			if _, ok := allowedScanningKeys[key]; !ok {
				return fmt.Errorf("scanning.%s: unknown config key", key)
			}
		}
	}
	if err := validateSecretRefs(root); err != nil {
		return err
	}
	_, err = Decode(bytes.NewReader(data))
	return err
}

func validateSecretRefs(root map[string]any) error {
	external, ok := root["external_databases"].(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"username", "password"} {
		value, ok := external[key]
		if !ok || value == nil {
			continue
		}
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("external_databases.%s: expected secret reference string", key)
		}
		if err := validateSecretRefValue("external_databases."+key, str, false); err != nil {
			return err
		}
	}
	return nil
}

func validateSecretRefValue(path, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s: required secret reference string", path)
		}
		return nil
	}
	if !strings.HasPrefix(value, "secretref://env/") {
		return fmt.Errorf("%s: sensitive values must use secretref://env/...", path)
	}
	return nil
}

type RuntimeConfig struct {
	SystemSettings   SystemSettings   `json:"system_settings"`
	Database         Database         `json:"database"`
	ExternalDatabase ExternalDatabase `json:"external_databases"`
	Scanning         Scanning         `json:"scanning"`
	Repositories     Repositories     `json:"repositories"`
	Security         Security         `json:"security"`
	API              API              `json:"api"`
	Auth             Auth             `json:"auth"`
	Workers          Workers          `json:"workers"`
	Modules          Modules          `json:"modules"`
	Logging          Logging          `json:"logging"`
}

type SystemSettings struct {
	AppName string `json:"app_name"`
	Version string `json:"version"`
	Mode    string `json:"mode"`
}

type Database struct {
	DatabaseType string `json:"database_type"`
	DatabasePath string `json:"database_path"`
}

type ExternalDatabase struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider"`
	EngineFlavor string `json:"engine_flavor"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	DatabaseName string `json:"database_name"`
}

type Scanning struct {
	GlobalScan []GlobalScanRoot `json:"global_scan"`
	Security   SecurityScan     `json:"security_scan"`
	Toolchain  Toolchain        `json:"toolchain"`
}

type GlobalScanRoot struct {
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	Schedule  bool   `json:"schedule"`
	Frequency string `json:"frequency"`
}

type SecurityScan struct {
	Modules []string `json:"modules"`
}

type Toolchain struct {
	VersionPolicy string   `json:"version_policy"`
	ProfilePaths  []string `json:"profile_paths"`
}

type Repositories struct {
	DefaultAuthType string `json:"default_auth_type"`
	PollInterval    string `json:"poll_interval_default"`
	AutoSyncDefault bool   `json:"auto_sync_default"`
}

type Security struct {
	ActiveRuleSetID *string `json:"active_rule_set_id"`
}

type API struct {
	ListenAddress string `json:"listen_address"`
}

type Auth struct {
	LocalEnabled bool `json:"local_enabled"`
}

type Workers struct {
	Enabled     bool `json:"enabled"`
	Concurrency int  `json:"concurrency"`
}

type Modules struct {
	Enabled []string `json:"enabled"`
}

type Logging struct {
	Level   string `json:"level"`
	Format  string `json:"format"`
	LogPath string `json:"log_path"`
}

type Entry struct {
	Key       string
	Value     string
	ValueType string
}

func Decode(r io.Reader) (RuntimeConfig, error) {
	return decode(r, false)
}

func DecodeStrict(r io.Reader) (RuntimeConfig, error) {
	return decode(r, true)
}

func decode(r io.Reader, rejectTrailing bool) (RuntimeConfig, error) {
	var cfg RuntimeConfig
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decode config: %w", err)
	}
	if rejectTrailing {
		var extra any
		if err := decoder.Decode(&extra); err != nil {
			if err != io.EOF {
				return cfg, fmt.Errorf("decode config: %w", err)
			}
		} else {
			return cfg, fmt.Errorf("decode config: request body must contain a single JSON object")
		}
	}
	cfg = Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func Normalize(cfg RuntimeConfig) RuntimeConfig {
	cfg.Database.DatabasePath = cleanPath(cfg.Database.DatabasePath)
	cfg.Logging.LogPath = cleanPath(cfg.Logging.LogPath)
	if cfg.Scanning.Toolchain.VersionPolicy == "" {
		cfg.Scanning.Toolchain.VersionPolicy = "certified_only"
	}
	for i, path := range cfg.Scanning.Toolchain.ProfilePaths {
		cfg.Scanning.Toolchain.ProfilePaths[i] = cleanPath(path)
	}
	return cfg
}

func Validate(cfg RuntimeConfig) error {
	cfg = Normalize(cfg)
	if strings.TrimSpace(cfg.SystemSettings.AppName) == "" {
		return fmt.Errorf("system_settings.app_name: required")
	}
	if !oneOf(cfg.SystemSettings.Mode, "server", "local") {
		return fmt.Errorf("system_settings.mode: expected server or local")
	}
	if cfg.Database.DatabaseType != "sqlite" {
		return fmt.Errorf("database.database_type: expected sqlite")
	}
	if cleanPath(cfg.Database.DatabasePath) == "" {
		return fmt.Errorf("database.database_path: required normalized path")
	}
	if cfg.ExternalDatabase.Enabled {
		if !oneOf(cfg.ExternalDatabase.Provider, "postgresql", "mysql", "mssql") {
			return fmt.Errorf("external_databases.provider: unsupported provider")
		}
		if cfg.ExternalDatabase.Host == "" || cfg.ExternalDatabase.Port <= 0 || cfg.ExternalDatabase.Port > 65535 || cfg.ExternalDatabase.DatabaseName == "" {
			return fmt.Errorf("external_databases: host, port and database_name are required")
		}
	}
	if err := validateSecretRefValue("external_databases.username", cfg.ExternalDatabase.Username, cfg.ExternalDatabase.Enabled); err != nil {
		return err
	}
	if err := validateSecretRefValue("external_databases.password", cfg.ExternalDatabase.Password, cfg.ExternalDatabase.Enabled); err != nil {
		return err
	}
	if cfg.ExternalDatabase.EngineFlavor != "" && !oneOf(cfg.ExternalDatabase.EngineFlavor, "standard", "aurora") {
		return fmt.Errorf("external_databases.engine_flavor: unsupported engine flavor")
	}
	for i, root := range cfg.Scanning.GlobalScan {
		if cleanPath(root.RootPath) == "" || !filepath.IsAbs(root.RootPath) {
			return fmt.Errorf("scanning.global_scan[%d].root_path: expected absolute normalized path", i)
		}
		if !oneOf(root.Frequency, "daily", "weekly", "monthly") {
			return fmt.Errorf("scanning.global_scan[%d].frequency: unsupported frequency", i)
		}
	}
	if len(cfg.Scanning.Security.Modules) == 0 {
		return fmt.Errorf("scanning.security_scan.modules: must not be empty")
	}
	seen := map[string]struct{}{}
	for _, module := range cfg.Scanning.Security.Modules {
		if strings.TrimSpace(module) == "" {
			return fmt.Errorf("scanning.security_scan.modules: empty module")
		}
		if _, ok := seen[module]; ok {
			return fmt.Errorf("scanning.security_scan.modules: duplicate module %q", module)
		}
		seen[module] = struct{}{}
	}
	if !oneOf(cfg.Scanning.Toolchain.VersionPolicy, "certified_only", "compatible_range", "latest_best_effort") {
		return fmt.Errorf("scanning.toolchain.version_policy: unsupported policy")
	}
	for i, path := range cfg.Scanning.Toolchain.ProfilePaths {
		if cleanPath(path) == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("scanning.toolchain.profile_paths[%d]: expected absolute normalized path", i)
		}
	}
	if !oneOf(cfg.Repositories.DefaultAuthType, "ssh", "https", "token") {
		return fmt.Errorf("repositories.default_auth_type: unsupported auth type")
	}
	if _, err := time.ParseDuration(cfg.Repositories.PollInterval); err != nil {
		return fmt.Errorf("repositories.poll_interval_default: %w", err)
	}
	if cfg.Workers.Concurrency <= 0 {
		return fmt.Errorf("workers.concurrency: must be positive")
	}
	if cfg.Database.DatabaseType == "sqlite" && cfg.Workers.Concurrency != 1 {
		return fmt.Errorf("sqlite_worker_concurrency_unsupported")
	}
	if err := ValidateModuleNames(cfg.Modules.Enabled); err != nil {
		return err
	}
	if host, port, err := net.SplitHostPort(cfg.API.ListenAddress); err != nil || host == "" || port == "" {
		return fmt.Errorf("api.listen_address: expected host:port")
	}
	if !oneOf(cfg.Logging.Level, "debug", "info", "warn", "error") {
		return fmt.Errorf("logging.level: unsupported level")
	}
	if !oneOf(cfg.Logging.Format, "json", "text") {
		return fmt.Errorf("logging.format: unsupported format")
	}
	if cleanPath(cfg.Logging.LogPath) == "" {
		return fmt.Errorf("logging.log_path: required normalized path")
	}
	return nil
}

func ValidateModuleNames(names []string) error {
	registered := InitialModuleNames()
	seen := map[string]struct{}{}
	for _, name := range names {
		if _, ok := registered[name]; !ok {
			return fmt.Errorf("modules.enabled: unknown module %q", name)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("modules.enabled: duplicate module %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func InitialModuleNames() map[string]struct{} {
	names := []string{"core", "worker-runtime", "config-manager", "module-runtime", "status-monitor", "global-scanner", "repository-manager", "project-scanner", "security-validator", "auth", "web"}
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}

func Flatten(cfg RuntimeConfig) ([]Entry, error) {
	cfg = Normalize(cfg)
	entries := []Entry{
		stringEntry("system_settings.app_name", cfg.SystemSettings.AppName),
		stringEntry("system_settings.version", cfg.SystemSettings.Version),
		stringEntry("system_settings.mode", cfg.SystemSettings.Mode),
		stringEntry("database.database_type", cfg.Database.DatabaseType),
		stringEntry("database.database_path", cfg.Database.DatabasePath),
		boolEntry("external_databases.enabled", cfg.ExternalDatabase.Enabled),
		stringEntry("external_databases.provider", cfg.ExternalDatabase.Provider),
		stringEntry("external_databases.engine_flavor", cfg.ExternalDatabase.EngineFlavor),
		stringEntry("external_databases.host", cfg.ExternalDatabase.Host),
		intEntry("external_databases.port", cfg.ExternalDatabase.Port),
		stringEntry("external_databases.username", cfg.ExternalDatabase.Username),
		stringEntry("external_databases.password", cfg.ExternalDatabase.Password),
		stringEntry("external_databases.database_name", cfg.ExternalDatabase.DatabaseName),
		jsonEntry("scanning.global_scan", cfg.Scanning.GlobalScan),
		jsonEntry("scanning.security_scan.modules", cfg.Scanning.Security.Modules),
		jsonEntry("scanning.toolchain.profile_paths", cfg.Scanning.Toolchain.ProfilePaths),
		stringEntry("scanning.toolchain.version_policy", cfg.Scanning.Toolchain.VersionPolicy),
		stringEntry("repositories.default_auth_type", cfg.Repositories.DefaultAuthType),
		stringEntry("repositories.poll_interval_default", cfg.Repositories.PollInterval),
		boolEntry("repositories.auto_sync_default", cfg.Repositories.AutoSyncDefault),
		jsonEntry("security.active_rule_set_id", cfg.Security.ActiveRuleSetID),
		stringEntry("api.listen_address", cfg.API.ListenAddress),
		boolEntry("auth.local_enabled", cfg.Auth.LocalEnabled),
		boolEntry("workers.enabled", cfg.Workers.Enabled),
		intEntry("workers.concurrency", cfg.Workers.Concurrency),
		jsonEntry("modules.enabled", cfg.Modules.Enabled),
		stringEntry("logging.level", cfg.Logging.Level),
		stringEntry("logging.format", cfg.Logging.Format),
		stringEntry("logging.log_path", cfg.Logging.LogPath),
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

func StorageProfile(cfg RuntimeConfig) (provider, engineFlavor, payload, fingerprint string, err error) {
	if cfg.ExternalDatabase.Enabled {
		provider = normalizeProviderName(cfg.ExternalDatabase.Provider)
		engineFlavor = cfg.ExternalDatabase.EngineFlavor
		payloadBytes, marshalErr := json.Marshal(cfg.ExternalDatabase)
		if marshalErr != nil {
			err = marshalErr
			return
		}
		payload = string(payloadBytes)
		fingerprint = fingerprintFor(provider, cfg.ExternalDatabase.Host, fmt.Sprintf("%d", cfg.ExternalDatabase.Port), cfg.ExternalDatabase.DatabaseName)
		return
	}
	provider = cfg.Database.DatabaseType
	payloadBytes, marshalErr := json.Marshal(cfg.Database)
	if marshalErr != nil {
		err = marshalErr
		return
	}
	payload = string(payloadBytes)
	fingerprint = fingerprintFor(provider, cleanPath(cfg.Database.DatabasePath))
	return
}

func MaskSensitiveValue(key, value string) any {
	if !isSensitiveKey(key) {
		return value
	}
	if strings.HasPrefix(value, "secretref://env/") {
		return map[string]any{"masked": true, "ref_type": "env"}
	}
	return map[string]any{"masked": true}
}

func isSensitiveKey(key string) bool {
	return key == "external_databases.username" || key == "external_databases.password"
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func stringEntry(key, value string) Entry {
	return Entry{Key: key, Value: value, ValueType: "string"}
}

func boolEntry(key string, value bool) Entry {
	if value {
		return Entry{Key: key, Value: "true", ValueType: "bool"}
	}
	return Entry{Key: key, Value: "false", ValueType: "bool"}
}

func intEntry(key string, value int) Entry {
	return Entry{Key: key, Value: fmt.Sprintf("%d", value), ValueType: "int"}
}

func jsonEntry(key string, value any) Entry {
	data, _ := json.Marshal(value)
	return Entry{Key: key, Value: string(data), ValueType: "json"}
}

func fingerprintFor(provider string, parts ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(provider))
	for _, part := range parts {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	return "db:" + hex.EncodeToString(hash.Sum(nil))
}
