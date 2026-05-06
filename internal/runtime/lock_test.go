package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireLockWritesMetadataAndReleaseRemovesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thelper-runtime.lock")
	lock, err := AcquireLock(path, LockMetadata{
		InstanceID:                "runtime_test",
		APIListenAddress:          "127.0.0.1:1",
		ConfigDatabaseFingerprint: "db:test",
		StartedAt:                 time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	var metadata LockMetadata
	if err := json.NewDecoder(file).Decode(&metadata); err != nil {
		t.Fatalf("decode lock metadata: %v", err)
	}
	_ = file.Close()
	if metadata.SchemaVersion != LockSchemaVersion || metadata.PID != os.Getpid() || metadata.ConfigDatabaseFingerprint != "db:test" {
		t.Fatalf("unexpected lock metadata: %+v", metadata)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock file still exists or unexpected stat error: %v", err)
	}
}

func TestAcquireLockReplacesStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thelper-runtime.lock")
	stale := LockMetadata{
		SchemaVersion:             LockSchemaVersion,
		InstanceID:                "runtime_stale",
		PID:                       -1,
		Host:                      "test",
		APIListenAddress:          "127.0.0.1:1",
		ConfigDatabaseFingerprint: "db:stale",
		StartedAt:                 time.Now().UTC(),
		UpdatedAt:                 time.Now().UTC(),
	}
	writeLockFile(t, path, stale)

	lock, err := AcquireLock(path, LockMetadata{
		InstanceID:                "runtime_new",
		APIListenAddress:          "127.0.0.1:2",
		ConfigDatabaseFingerprint: "db:new",
	})
	if err != nil {
		t.Fatalf("acquire after stale lock: %v", err)
	}
	defer lock.Release()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open replaced lock: %v", err)
	}
	defer file.Close()
	var metadata LockMetadata
	if err := json.NewDecoder(file).Decode(&metadata); err != nil {
		t.Fatalf("decode replaced lock: %v", err)
	}
	if metadata.InstanceID != "runtime_new" || metadata.ConfigDatabaseFingerprint != "db:new" {
		t.Fatalf("stale lock was not replaced: %+v", metadata)
	}
}

func TestAcquireLockFailsClosedWhenPIDLiveButProbeFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thelper-runtime.lock")
	writeLockFile(t, path, LockMetadata{
		SchemaVersion:             LockSchemaVersion,
		InstanceID:                "runtime_live",
		PID:                       os.Getpid(),
		Host:                      "test",
		APIListenAddress:          "127.0.0.1:1",
		ConfigDatabaseFingerprint: "db:live",
		StartedAt:                 time.Now().UTC(),
		UpdatedAt:                 time.Now().UTC(),
	})

	_, err := AcquireLock(path, LockMetadata{
		InstanceID:                "runtime_new",
		APIListenAddress:          "127.0.0.1:2",
		ConfigDatabaseFingerprint: "db:new",
	})
	if err == nil || !strings.Contains(err.Error(), "pid is live but health probe failed") {
		t.Fatalf("expected fail-closed live pid error, got %v", err)
	}
}

func writeLockFile(t *testing.T, path string, metadata LockMetadata) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		t.Fatal(err)
	}
}
