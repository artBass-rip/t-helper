package modules

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

type failingLifecycle struct {
	err error
}

func (l failingLifecycle) Start(context.Context) error  { return nil }
func (l failingLifecycle) Stop(context.Context) error   { return nil }
func (l failingLifecycle) Reload(context.Context) error { return l.err }
func (l failingLifecycle) Health(context.Context) error { return nil }

func TestReloadFailurePersistsFailedStateAndError(t *testing.T) {
	ctx := context.Background()
	handle := openMigratedSQLiteInternal(t)
	defer handle.Close()
	cause := errors.New("reload failed")
	store := NewStore(handle)
	store.registry["core"] = Definition{Name: "core", Available: true, Lifecycle: failingLifecycle{err: cause}}
	if err := store.Seed(ctx, []string{"core"}); err != nil {
		t.Fatalf("seed modules: %v", err)
	}
	if _, err := store.Reload(ctx, "core", "test"); err == nil {
		t.Fatal("expected reload failure")
	} else if !errors.Is(err, ErrModuleLifecycle) {
		t.Fatalf("reload error = %v, want ErrModuleLifecycle", err)
	}
	state, err := store.get(ctx, "core")
	if err != nil {
		t.Fatalf("get core state: %v", err)
	}
	if state.State != StateFailed {
		t.Fatalf("state = %q, want failed", state.State)
	}
	if state.Details["last_error"] != cause.Error() {
		t.Fatalf("last_error = %#v, want %q", state.Details["last_error"], cause.Error())
	}
}

func openMigratedSQLiteInternal(t *testing.T) *storage.Handle {
	t.Helper()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(context.Background(), storage.Config{Provider: "sqlite", DSN: filepath.Join(t.TempDir(), "stage02-internal.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := provider.Migrate(context.Background(), handle); err != nil {
		handle.Close()
		t.Fatalf("migrate sqlite: %v", err)
	}
	return handle
}
