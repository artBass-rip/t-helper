package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/httpapi"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestStage02HTTPConfigAndModuleFlow(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()

	configStore := config.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	if err := moduleStore.Seed(ctx); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	health := runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))
	handler := httpapi.New(
		httpapi.NewHealthHandler(health),
		httpapi.NewConfigHandler(configStore),
		httpapi.NewModulesHandler(configStore, moduleStore),
	)

	payload := readFile(t, "../../config.example.json")
	put := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config status = %d body = %s", rec.Code, rec.Body.String())
	}
	var importResult config.ImportResult
	if err := json.NewDecoder(rec.Body).Decode(&importResult); err != nil {
		t.Fatalf("decode import result: %v", err)
	}
	if importResult.SchemaVersion != "config_import.result.v1" || len(importResult.AppliedKeys) == 0 {
		t.Fatalf("unexpected import result: %+v", importResult)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config status = %d body = %s", rec.Code, rec.Body.String())
	}
	var active map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&active); err != nil {
		t.Fatalf("decode active config: %v", err)
	}
	external := active["external_databases"].(map[string]any)
	password := external["password"].(map[string]any)
	if password["masked"] != true || password["ref_type"] != "env" {
		t.Fatalf("password not masked: %#v", password)
	}
	toolchain := active["scanning"].(map[string]any)["toolchain"].(map[string]any)
	if toolchain["version_policy"] != "certified_only" {
		t.Fatalf("toolchain config not preserved: %#v", toolchain)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`{"keys":["logging.level","api.listen_address"],"reason":"test"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/modules/reload status = %d body = %s", rec.Code, rec.Body.String())
	}
	var reload config.ReloadResult
	if err := json.NewDecoder(rec.Body).Decode(&reload); err != nil {
		t.Fatalf("decode reload result: %v", err)
	}
	if len(reload.AcceptedKeys) != 1 || reload.AcceptedKeys[0] != "logging.level" || len(reload.AppliedKeys) != 0 || len(reload.RestartRequiredKeys) != 1 || reload.RestartRequiredKeys[0] != "api.listen_address" {
		t.Fatalf("unexpected reload result: %+v", reload)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`{`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid reload JSON status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`{"keys":["logging.level"],"unknown":true}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown reload field status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`{"keys":["logging.levl"],"reason":"test"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown reload key status = %d body = %s", rec.Code, rec.Body.String())
	}
	var failedReload config.ReloadResult
	if err := json.NewDecoder(rec.Body).Decode(&failedReload); err != nil {
		t.Fatalf("decode failed reload result: %v", err)
	}
	if len(failedReload.FailedKeys) != 1 || failedReload.FailedKeys[0] != "logging.levl" {
		t.Fatalf("unexpected failed reload result: %+v", failedReload)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`{"module_name":"global-scanner","reason":"test"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unavailable module reload status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`{"module_name":"config-manager","reason":"test"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("available module reload status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/modules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/modules status = %d body = %s", rec.Code, rec.Body.String())
	}
	var moduleList struct {
		Items []modules.ModuleState `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&moduleList); err != nil {
		t.Fatalf("decode modules: %v", err)
	}
	if len(moduleList.Items) != len(modules.InitialRegistry()) {
		t.Fatalf("module count = %d, want %d", len(moduleList.Items), len(modules.InitialRegistry()))
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/restart", bytes.NewReader([]byte(`{"module_name":"global-scanner","reason":"test"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unavailable restart status = %d body = %s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if apiErr.Error.Code != "module_unavailable" || apiErr.Error.CorrelationID == "" {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/restart", bytes.NewReader([]byte(`{"reason":"test"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing module restart status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/restart", bytes.NewReader([]byte(`{"module_name":"does-not-exist"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown restart module status = %d body = %s", rec.Code, rec.Body.String())
	}
	apiErr = struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}{}
	if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode unknown module error: %v", err)
	}
	if apiErr.Error.Code != "validation_error" {
		t.Fatalf("unknown module error code = %q, want validation_error", apiErr.Error.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/restart", bytes.NewReader([]byte(`{"module_name":"config-manager"} {}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing restart payload status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/restart", bytes.NewReader([]byte(`{"module_name":"config-manager","reason":"test"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("available restart status = %d body = %s", rec.Code, rec.Body.String())
	}
	var restart modules.RestartResult
	if err := json.NewDecoder(rec.Body).Decode(&restart); err != nil {
		t.Fatalf("decode restart result: %v", err)
	}
	if restart.SchemaVersion != "module_restart.result.v1" || restart.NewState != modules.StateRunning {
		t.Fatalf("unexpected restart result: %+v", restart)
	}
}

func TestStage02HTTPRejectsInvalidConfigAtomically(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	configStore := config.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	if err := moduleStore.Seed(ctx); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewConfigHandler(configStore),
		httpapi.NewModulesHandler(configStore, moduleStore),
	)

	body := []byte(`{"system_settings":{"app_name":"t-helper","mode":"local"},"database":{"database_type":"sqlite","database_path":"/tmp/t.db"},"external_databases":{"enabled":false},"scanning":{"globalScan":[]},"repositories":{"default_auth_type":"ssh","poll_interval_default":"15m"},"api":{"listen_address":"127.0.0.1:8080"},"workers":{"enabled":true,"concurrency":1},"modules":{"enabled":["core"]},"logging":{"level":"info","format":"json","log_path":"/tmp"}}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid PUT status = %d body = %s", rec.Code, rec.Body.String())
	}
	var count int
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM config_entries").Scan(&count); err != nil {
		t.Fatalf("count config entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("config entries after rejected import = %d, want 0", count)
	}

	cfg, err := config.Decode(bytes.NewReader(readFile(t, "../../config.example.json")))
	if err != nil {
		t.Fatalf("decode example: %v", err)
	}
	cfg.ExternalDatabase.Enabled = false
	cfg.ExternalDatabase.Username = "admin"
	cfg.ExternalDatabase.Password = "secret"
	sensitiveBody, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal sensitive config: %v", err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(sensitiveBody)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sensitive literal PUT status = %d body = %s", rec.Code, rec.Body.String())
	}
	if err := handle.DB.QueryRowContext(ctx, "SELECT count(*) FROM config_entries").Scan(&count); err != nil {
		t.Fatalf("count config entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("config entries after rejected sensitive import = %d, want 0", count)
	}
}

func TestStage02HTTPRejectsTrailingConfigPayloadAndNullModuleReload(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLite(t)
	defer handle.Close()
	configStore := config.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	if err := moduleStore.Seed(ctx); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	handler := httpapi.New(
		httpapi.NewHealthHandler(runtime.NewHealthService("runtime_test", "local", testStartedAt(), runtime.NewStorageHealthSource(handle))),
		httpapi.NewConfigHandler(configStore),
		httpapi.NewModulesHandler(configStore, moduleStore),
	)

	payload := append(readFile(t, "../../config.example.json"), []byte(` {}`)...)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(payload)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing config payload status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/modules/reload", bytes.NewReader([]byte(`null`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("null module reload status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func openMigratedSQLite(t *testing.T) *storage.Handle {
	t.Helper()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(context.Background(), storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "stage02-http.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := provider.Migrate(context.Background(), handle); err != nil {
		handle.Close()
		t.Fatalf("migrate sqlite: %v", err)
	}
	return handle
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testStartedAt() time.Time {
	return time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
}
