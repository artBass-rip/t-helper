package jobs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

var benchmarkConfigReloadPayload = json.RawMessage(`{"schema_version":"jobs.config_reload.payload.v1","keys":["logging.level"]}`)

func BenchmarkStoreClaimNext(b *testing.B) {
	ctx := context.Background()
	store := openBenchmarkStore(b)
	for i := 0; i < b.N; i++ {
		if _, err := store.Enqueue(ctx, jobs.EnqueueRequest{
			JobType: "config_reload",
			Payload: benchmarkConfigReloadPayload,
		}); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok, err := store.ClaimNext(ctx, jobs.ClaimOptions{
			WorkerID:      "benchmark:worker",
			LeaseDuration: time.Minute,
		}); err != nil {
			b.Fatalf("claim next: %v", err)
		} else if !ok {
			b.Fatal("expected queued job")
		}
	}
}

func BenchmarkStoreRefreshWorkflowStatus1000(b *testing.B) {
	ctx := context.Background()
	store := openBenchmarkStore(b)
	const workflowID = "benchmark_workflow"
	const groupID = "benchmark_group"
	for i := 0; i < 1000; i++ {
		if _, err := store.Enqueue(ctx, jobs.EnqueueRequest{
			JobType:    "config_reload",
			WorkflowID: workflowID,
			JobGroupID: groupID,
			Payload:    benchmarkConfigReloadPayload,
		}); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.RefreshWorkflowStatus(ctx, groupID, workflowID); err != nil {
			b.Fatalf("refresh workflow status: %v", err)
		}
	}
}

func openBenchmarkStore(b *testing.B) *jobs.Store {
	b.Helper()
	ctx := context.Background()
	provider := sqlite.NewProvider()
	handle, err := provider.Open(ctx, storage.Config{Provider: "sqlite", DSN: ":memory:"})
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	b.Cleanup(func() { _ = handle.Close() })
	if err := provider.Migrate(ctx, handle); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	return jobs.NewStore(handle)
}
