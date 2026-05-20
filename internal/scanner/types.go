package scanner

import (
	"encoding/json"
	"time"
)

const (
	GlobalScanPayloadSchema       = "jobs.global_scan.payload.v1"
	GlobalScanResultSchema        = "jobs.global_scan.result.v1"
	ProjectDiscoveryPayloadSchema = "jobs.project_discovery.payload.v1"
	ProjectDiscoveryResultSchema  = "jobs.project_discovery.result.v1"

	ProjectStatusActive   = "active"
	ProjectStatusMissing  = "missing"
	ProjectStatusDisabled = "disabled"

	RepositoryProviderGeneric = "generic"
	RepositoryHostLocal       = "local"
	RepositoryStatusActive    = "active"
	DiscoverySourceFilesystem = "filesystem"

	LinkTypeSameRepository = "same_repository"
	TerraformMarkerGlob    = "*.tf"
)

type RootPath struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Path              string    `json:"path"`
	Enabled           bool      `json:"enabled"`
	ScheduleEnabled   bool      `json:"schedule_enabled"`
	ScheduleFrequency string    `json:"schedule_frequency,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type RootPathInput struct {
	ID                string `json:"id,omitempty"`
	Name              string `json:"name,omitempty"`
	Path              string `json:"path"`
	Enabled           *bool  `json:"enabled,omitempty"`
	ScheduleEnabled   *bool  `json:"schedule_enabled,omitempty"`
	ScheduleFrequency string `json:"schedule_frequency,omitempty"`
}

type Project struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Path               string    `json:"path"`
	RelativePath       string    `json:"relative_path"`
	RootPathID         string    `json:"root_path_id"`
	TerraformMarker    string    `json:"terraform_marker"`
	Status             string    `json:"status"`
	RepositoryID       string    `json:"repository_id,omitempty"`
	EnvironmentID      string    `json:"environment_id,omitempty"`
	DefaultWorkspaceID string    `json:"default_workspace_id,omitempty"`
	DetectedAt         time.Time `json:"detected_at"`
	LastSeenAt         time.Time `json:"last_seen_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Repository struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Provider        string    `json:"provider"`
	ProviderHost    string    `json:"provider_host"`
	FullPath        string    `json:"full_path"`
	RootPathID      string    `json:"root_path_id,omitempty"`
	LocalPath       string    `json:"local_path,omitempty"`
	Status          string    `json:"status"`
	DiscoverySource string    `json:"discovery_source"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type IgnoreRule struct {
	ID        string    `json:"id"`
	ScopeType string    `json:"scope_type"`
	ScopeID   string    `json:"scope_id,omitempty"`
	Pattern   string    `json:"pattern"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IgnoreRuleInput struct {
	ID        string `json:"id,omitempty"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id,omitempty"`
	Pattern   string `json:"pattern"`
	Origin    string `json:"origin,omitempty"`
}

type Environment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Workspace struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	EnvironmentID string    `json:"environment_id"`
	Name          string    `json:"name"`
	IsDefault     bool      `json:"is_default"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ListOptions struct {
	Limit  int
	Cursor string
}

type ProjectListOptions struct {
	ListOptions
	RootPathID   string
	RepositoryID string
	Status       string
}

type IgnoreRuleListOptions struct {
	ListOptions
	ScopeType string
	ScopeID   string
}

type WorkspaceListOptions struct {
	ListOptions
	ProjectID     string
	EnvironmentID string
}

type Page[T any] struct {
	Items      []T
	NextCursor string
}

type GlobalScanPayload struct {
	SchemaVersion         string   `json:"schema_version"`
	RootPathIDs           []string `json:"root_path_ids,omitempty"`
	Reason                string   `json:"reason,omitempty"`
	FollowSymlinks        bool     `json:"follow_symlinks"`
	IgnoreRulesSnapshotID string   `json:"ignore_rules_snapshot_id,omitempty"`
}

type GlobalScanResult struct {
	SchemaVersion                string   `json:"schema_version"`
	RootPathIDs                  []string `json:"root_path_ids"`
	ProjectsCreated              int      `json:"projects_created"`
	ProjectsUpdated              int      `json:"projects_updated"`
	ProjectsMarkedMissing        int      `json:"projects_marked_missing"`
	ProjectDiscoveryJobsEnqueued int      `json:"project_discovery_jobs_enqueued"`
	DirectoriesSkipped           int      `json:"directories_skipped"`
	SymlinksSkipped              int      `json:"symlinks_skipped"`
	ErrorsCount                  int      `json:"errors_count"`
}

type ProjectDiscoveryPayload struct {
	SchemaVersion string `json:"schema_version"`
	ProjectID     string `json:"project_id"`
	RootPathID    string `json:"root_path_id"`
	RelativePath  string `json:"relative_path"`
	Reason        string `json:"reason,omitempty"`
}

type ProjectDiscoveryResult struct {
	SchemaVersion         string   `json:"schema_version"`
	ProjectID             string   `json:"project_id"`
	GitRepositoryDetected bool     `json:"git_repository_detected"`
	RepositoryID          string   `json:"repository_id,omitempty"`
	RepositoryCreated     bool     `json:"repository_created"`
	RepositoryUpdated     bool     `json:"repository_updated"`
	LinkedProjectIDs      []string `json:"linked_project_ids"`
	LinksCreated          int      `json:"links_created"`
	GitMarkerType         string   `json:"git_marker_type,omitempty"`
}

func marshalJSON(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}
