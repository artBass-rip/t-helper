package worker

import (
	"context"
	"testing"

	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func TestSQLiteWorkerLockRejectsSecondActiveWorker(t *testing.T) {
	ctx := context.Background()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(ctx, storage.Config{Provider: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer handle.Close()

	first, err := acquireSQLiteWorkerLock(handle)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer first.Release()

	second, err := acquireSQLiteWorkerLock(handle)
	if err == nil {
		second.Release()
		t.Fatal("expected second worker lock acquisition to fail")
	}
}
