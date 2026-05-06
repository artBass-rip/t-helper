package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const LockSchemaVersion = "runtime_lock.v1"

type LockMetadata struct {
	SchemaVersion             string    `json:"schema_version"`
	InstanceID                string    `json:"instance_id"`
	PID                       int       `json:"pid"`
	Host                      string    `json:"host"`
	APIListenAddress          string    `json:"api_listen_address"`
	StartedAt                 time.Time `json:"started_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
	ConfigDatabaseFingerprint string    `json:"config_database_fingerprint"`
}

type RuntimeLock struct {
	path string
	file *os.File
}

func AcquireLock(path string, metadata LockMetadata) (*RuntimeLock, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	metadata.SchemaVersion = LockSchemaVersion
	metadata.PID = os.Getpid()
	host, _ := os.Hostname()
	metadata.Host = host
	now := time.Now().UTC()
	if metadata.StartedAt.IsZero() {
		metadata.StartedAt = now
	}
	metadata.UpdatedAt = now

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if err := json.NewEncoder(file).Encode(metadata); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, err
		}
		return &RuntimeLock{path: path, file: file}, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	existing, readErr := readLock(path)
	if readErr != nil {
		return nil, fmt.Errorf("runtime lock exists but cannot be read: %w", readErr)
	}
	if processLive(existing.PID) {
		if probeHealth(existing.APIListenAddress, existing.InstanceID, existing.ConfigDatabaseFingerprint) {
			return nil, fmt.Errorf("runtime already active at %s", existing.APIListenAddress)
		}
		return nil, fmt.Errorf("runtime lock pid is live but health probe failed")
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	return AcquireLock(path, metadata)
}

func (l *RuntimeLock) Release() error {
	if l == nil {
		return nil
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	return os.Remove(l.path)
}

func readLock(path string) (LockMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return LockMetadata{}, err
	}
	defer file.Close()
	var metadata LockMetadata
	if err := json.NewDecoder(file).Decode(&metadata); err != nil {
		return LockMetadata{}, err
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

func probeHealth(address, instanceID, fingerprint string) bool {
	if address == "" {
		return false
	}
	client := http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Get("http://" + address + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false
	}
	return status.InstanceID == instanceID && status.DatabaseFingerprint == fingerprint
}
