package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/artBass-rip/t-helper/internal/storage"
)

func TestAppHealthSmoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry := storageproviders.MVPRegistry()
	cfg := DefaultConfig()
	cfg.StorageDSN = filepath.Join(t.TempDir(), "stage01-smoke.db")
	app := New(cfg, registry, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	handle, err := registry.Open(ctx, storage.Config{Provider: cfg.StorageProvider, DSN: cfg.StorageDSN})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer handle.Close()
	if err := registry.Migrate(ctx, handle); err != nil {
		t.Fatalf("migrate storage: %v", err)
	}
	handler, err := app.BuildHandler(ctx, handle)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.ServeListener(ctx, listener, handler)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("server shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("server did not stop")
		}
	})

	var response *http.Response
	for i := 0; i < 50; i++ {
		response, err = http.Get("http://" + address + "/api/health")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /api/health: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", response.StatusCode)
	}

	var status runtime.HealthStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode health status: %v", err)
	}
	if status.SchemaVersion != runtime.HealthSchemaVersion || status.Readiness != runtime.ReadinessReady {
		t.Fatalf("unexpected health status: %+v", status)
	}
	if status.InstanceID == "" || status.DatabaseFingerprint == "" || status.StartedAt.IsZero() {
		t.Fatalf("missing required health fields: %+v", status)
	}
}
