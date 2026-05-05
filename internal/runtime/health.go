package runtime

import (
	"context"
	"time"
)

const HealthSchemaVersion = "health_status.v1"

type Readiness string

const (
	ReadinessReady    Readiness = "ready"
	ReadinessStarting Readiness = "starting"
	ReadinessDegraded Readiness = "degraded"
)

type HealthStatus struct {
	InstanceID          string    `json:"instance_id"`
	Mode                string    `json:"mode"`
	DatabaseFingerprint string    `json:"database_fingerprint"`
	StartedAt           time.Time `json:"started_at"`
	Readiness           Readiness `json:"readiness"`
	SchemaVersion       string    `json:"schema_version"`
}

type HealthSource interface {
	Ping(ctx context.Context) error
	Fingerprint() string
}

type HealthService struct {
	instanceID string
	mode       string
	startedAt  time.Time
	source     HealthSource
}

func NewHealthService(instanceID, mode string, startedAt time.Time, source HealthSource) *HealthService {
	return &HealthService{
		instanceID: instanceID,
		mode:       mode,
		startedAt:  startedAt.UTC(),
		source:     source,
	}
}

func (s *HealthService) Status(ctx context.Context) HealthStatus {
	readiness := ReadinessReady
	if s.source == nil || s.source.Ping(ctx) != nil {
		readiness = ReadinessDegraded
	}
	fingerprint := ""
	if s.source != nil {
		fingerprint = s.source.Fingerprint()
	}
	return HealthStatus{
		InstanceID:          s.instanceID,
		Mode:                s.mode,
		DatabaseFingerprint: fingerprint,
		StartedAt:           s.startedAt,
		Readiness:           readiness,
		SchemaVersion:       HealthSchemaVersion,
	}
}
