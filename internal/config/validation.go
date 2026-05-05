package config

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	var root map[string]any
	decoder := json.NewDecoder(r)
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
	return validateSecretRefs(root)
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
		if !strings.HasPrefix(str, "secretref://env/") {
			return fmt.Errorf("external_databases.%s: sensitive values must use secretref://env/...", key)
		}
	}
	return nil
}
