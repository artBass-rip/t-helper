package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const workerLockSchemaVersion = "worker_process_lock.v1"

type workerLockMetadata struct {
	SchemaVersion       string    `json:"schema_version"`
	PID                 int       `json:"pid"`
	Host                string    `json:"host"`
	WorkerID            string    `json:"worker_id"`
	DatabaseFingerprint string    `json:"database_fingerprint"`
	StartedAt           time.Time `json:"started_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type workerProcessLock struct {
	path string
	file *os.File
}

func acquireWorkerProcessLock(path string, metadata workerLockMetadata) (*workerProcessLock, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	metadata.SchemaVersion = workerLockSchemaVersion
	metadata.PID = os.Getpid()
	if metadata.StartedAt.IsZero() {
		metadata.StartedAt = now
	}
	metadata.UpdatedAt = now
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	metadata.Host = host

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if err := json.NewEncoder(file).Encode(metadata); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, err
		}
		return &workerProcessLock{path: path, file: file}, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	existing, readErr := readWorkerProcessLock(path)
	if readErr != nil {
		return nil, fmt.Errorf("worker process lock exists but cannot be read: %w", readErr)
	}
	if processLive(existing.PID) {
		return nil, fmt.Errorf("worker process already active for database %s", existing.DatabaseFingerprint)
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	return acquireWorkerProcessLock(path, metadata)
}

func (l *workerProcessLock) Release() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	return os.Remove(l.path)
}

func readWorkerProcessLock(path string) (workerLockMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return workerLockMetadata{}, err
	}
	defer file.Close()
	var metadata workerLockMetadata
	if err := json.NewDecoder(file).Decode(&metadata); err != nil {
		return workerLockMetadata{}, err
	}
	return metadata, nil
}

func processLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
