package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/runtime"
)

type fakeHealthSource struct{}

func (fakeHealthSource) Ping(context.Context) error { return nil }
func (fakeHealthSource) Fingerprint() string        { return "db:test" }

func TestHealthEndpointShape(t *testing.T) {
	startedAt := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	service := runtime.NewHealthService("runtime_test", "local", startedAt, fakeHealthSource{})
	server := New(NewHealthHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	var got runtime.HealthStatus
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got.InstanceID != "runtime_test" ||
		got.Mode != "local" ||
		got.DatabaseFingerprint != "db:test" ||
		got.Readiness != runtime.ReadinessReady ||
		got.SchemaVersion != runtime.HealthSchemaVersion {
		t.Fatalf("unexpected health response: %+v", got)
	}
	if got.StartedAt.IsZero() {
		t.Fatal("expected started_at")
	}
	if rec.Header().Get(CorrelationIDHeader) == "" {
		t.Fatal("expected correlation id header")
	}
}
