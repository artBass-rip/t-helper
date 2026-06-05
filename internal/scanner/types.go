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
	ProjectScanPayloadSchema      = "jobs.project_scan.payload.v1"
	ProjectScanResultSchema       = "jobs.project_scan.result.v1"
	SecurityScanPayloadSchema     = "jobs.security_validation_scan.payload.v1"
	SecurityScanResultSchema      = "jobs.security_validation_scan.result.v1"
	ProjectScanRefSchema          = "project_scan_ref.v1"
	ProjectScanAggregateSchema    = "project_scans.result.v1"
	FindingFingerprintSchema      = "security_finding.fingerprint.v1"

	ProjectStatusActive   = "active"
	ProjectStatusMissing  = "missing"
	ProjectStatusDisabled = "disabled"

	RepositoryProviderGeneric = "generic"
	RepositoryHostLocal       = "local"
	RepositoryStatusActive    = "active"
	DiscoverySourceFilesystem = "filesystem"

	LinkTypeSameRepository = "same_repository"
	TerraformMarkerGlob    = "*.tf"

	RootPathSourceAPI    = "api"
	RootPathSourceConfig = "config"

	ScanTypeTerraformStatic    = "terraform_static"
	ScanTypeTerraformValidate  = "terraform_validate"
	ScanTypeTerraformSecurity  = "terraform_security"
	ScanTypeTerraformFull      = "terraform_full"
	ScanTypeSecurityValidation = "security_validation"
	ProjectScanStatusQueued    = "queued"
	ProjectScanStatusRunning   = "running"
	ProjectScanStatusSucceeded = "succeeded"
	ProjectScanStatusFailed    = "failed"
	ProjectScanStatusPartial   = "partial"
	ProjectScanStatusCancelled = "cancelled"
	FindingStatusOpen          = "open"
	DefaultSecurityModuleTrivy = "trivy"
	DefaultSecurityRuleSetID   = "security_rule_set_mvp"
)

type RootPath struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Path              string    `json:"path"`
	Source            string    `json:"source"`
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
	Source            string `json:"source,omitempty"`
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

type ProjectLink struct {
	ID              string    `json:"id"`
	SourceProjectID string    `json:"source_project_id"`
	TargetProjectID string    `json:"target_project_id"`
	LinkType        string    `json:"link_type"`
	RepositoryID    string    `json:"repository_id,omitempty"`
	DetectedByJobID string    `json:"detected_by_job_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Repository struct {
	ID                       string     `json:"id"`
	Name                     string     `json:"name"`
	ProviderInstanceID       string     `json:"provider_instance_id,omitempty"`
	Provider                 string     `json:"provider"`
	ProviderHost             string     `json:"provider_host"`
	FullPath                 string     `json:"full_path"`
	CloneURL                 string     `json:"clone_url,omitempty"`
	DefaultBranch            string     `json:"default_branch,omitempty"`
	RootPathID               string     `json:"root_path_id,omitempty"`
	TargetDirectory          string     `json:"target_directory,omitempty"`
	LocalPath                string     `json:"local_path,omitempty"`
	AuthType                 string     `json:"auth_type,omitempty"`
	DefaultCredentialID      string     `json:"default_credential_id,omitempty"`
	Status                   string     `json:"status"`
	DiscoverySource          string     `json:"discovery_source"`
	SupersededByRepositoryID string     `json:"superseded_by_repository_id,omitempty"`
	IdentityConfirmedAt      *time.Time `json:"identity_confirmed_at,omitempty"`
	AutoSyncEnabled          bool       `json:"auto_sync_enabled"`
	WebhookEnabled           bool       `json:"webhook_enabled"`
	PollInterval             string     `json:"poll_interval,omitempty"`
	LastPullAt               *time.Time `json:"last_pull_at,omitempty"`
	LastError                string     `json:"last_error,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type IgnoreRule struct {
	ID        string    `json:"id"`
	ScopeType string    `json:"scope_type"`
	ScopeID   string    `json:"scope_id,omitempty"`
	Pattern   string    `json:"pattern"`
	Origin    string    `json:"origin"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IgnoreRuleInput struct {
	ID        string `json:"id,omitempty"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id,omitempty"`
	Pattern   string `json:"pattern"`
	Origin    string `json:"origin,omitempty"`
	SortOrder *int   `json:"sort_order,omitempty"`
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

type ProjectScanSettings struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	ScanEnabled       bool      `json:"scan_enabled"`
	ScheduleEnabled   bool      `json:"schedule_enabled"`
	ScheduleFrequency string    `json:"schedule_frequency,omitempty"`
	RunAfterClone     bool      `json:"run_after_clone"`
	RunAfterPull      bool      `json:"run_after_pull"`
	ScanType          string    `json:"scan_type"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ProjectSecurityScanSettings struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Enabled           bool      `json:"enabled"`
	EnabledModules    []string  `json:"enabled_modules"`
	ScheduleEnabled   bool      `json:"schedule_enabled"`
	ScheduleFrequency string    `json:"schedule_frequency,omitempty"`
	ValidateCode      bool      `json:"validate_code"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ProjectScanSettingsInput struct {
	ScanEnabled       *bool  `json:"scan_enabled,omitempty"`
	ScheduleEnabled   *bool  `json:"schedule_enabled,omitempty"`
	ScheduleFrequency string `json:"schedule_frequency,omitempty"`
	RunAfterClone     *bool  `json:"run_after_clone,omitempty"`
	RunAfterPull      *bool  `json:"run_after_pull,omitempty"`
	ScanType          string `json:"scan_type,omitempty"`
	Security          *struct {
		Enabled           *bool    `json:"enabled,omitempty"`
		EnabledModules    []string `json:"enabled_modules,omitempty"`
		ScheduleEnabled   *bool    `json:"schedule_enabled,omitempty"`
		ScheduleFrequency string   `json:"schedule_frequency,omitempty"`
		ValidateCode      *bool    `json:"validate_code,omitempty"`
	} `json:"security,omitempty"`
}

type ProjectSettingsResponse struct {
	ProjectScanSettings
	Security ProjectSecurityScanSettings `json:"security"`
}

type ProjectScan struct {
	ID            string          `json:"id"`
	JobID         string          `json:"job_id"`
	ProjectID     string          `json:"project_id"`
	RuleSetID     string          `json:"rule_set_id,omitempty"`
	ScanType      string          `json:"scan_type"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	StartedAt     *time.Time      `json:"started_at,omitempty"`
	FinishedAt    *time.Time      `json:"finished_at,omitempty"`
	ResultPayload json.RawMessage `json:"result_payload,omitempty"`
	ErrorMessage  string          `json:"error_message,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type SecurityRuleSet struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	SourceType string    `json:"source_type"`
	Checksum   string    `json:"checksum,omitempty"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SecurityRuleSetInput struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	SourceType string `json:"source_type"`
	Checksum   string `json:"checksum,omitempty"`
	Active     *bool  `json:"active,omitempty"`
}

type SecurityFinding struct {
	ID                       string          `json:"id"`
	ProjectID                string          `json:"project_id,omitempty"`
	RepositoryID             string          `json:"repository_id,omitempty"`
	WorkspaceID              string          `json:"workspace_id,omitempty"`
	JobID                    string          `json:"job_id,omitempty"`
	RuleSetID                string          `json:"rule_set_id,omitempty"`
	CheckType                string          `json:"check_type"`
	RuleID                   string          `json:"rule_id"`
	Severity                 string          `json:"severity"`
	Status                   string          `json:"status"`
	FilePath                 string          `json:"file_path,omitempty"`
	ResourceRef              string          `json:"resource_ref,omitempty"`
	Title                    string          `json:"title"`
	Description              string          `json:"description,omitempty"`
	Remediation              string          `json:"remediation,omitempty"`
	Fingerprint              string          `json:"fingerprint"`
	FingerprintSchemaVersion string          `json:"fingerprint_schema_version"`
	FingerprintComponents    json.RawMessage `json:"fingerprint_components"`
	FirstSeenAt              time.Time       `json:"first_seen_at"`
	LastSeenAt               time.Time       `json:"last_seen_at"`
	DetectedAt               time.Time       `json:"detected_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

type ToolProfile struct {
	ID                 string          `json:"id"`
	Tool               string          `json:"tool"`
	ProfileID          string          `json:"profile_id"`
	ProfileVersion     string          `json:"profile_version"`
	SchemaVersion      string          `json:"schema_version"`
	SourceType         string          `json:"source_type"`
	SourcePath         string          `json:"source_path,omitempty"`
	Checksum           string          `json:"checksum,omitempty"`
	CertifiedVersions  json.RawMessage `json:"certified_versions"`
	CompatibleVersions json.RawMessage `json:"compatible_versions"`
	Active             bool            `json:"active"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type ToolProfileValidationResult struct {
	ID               string          `json:"id"`
	ToolProfileID    string          `json:"tool_profile_id,omitempty"`
	Tool             string          `json:"tool"`
	ToolVersion      string          `json:"tool_version,omitempty"`
	FixtureSet       string          `json:"fixture_set,omitempty"`
	ValidationStatus string          `json:"validation_status"`
	Diagnostics      json.RawMessage `json:"diagnostics"`
	CreatedAt        time.Time       `json:"created_at"`
}

type ToolProfileValidateInput struct {
	ProfilePath    string          `json:"profile_path,omitempty"`
	ProfilePayload json.RawMessage `json:"profile_payload,omitempty"`
	FixtureSet     string          `json:"fixture_set,omitempty"`
}

type ToolProfileImportInput struct {
	ProfilePath    string          `json:"profile_path,omitempty"`
	ProfilePayload json.RawMessage `json:"profile_payload,omitempty"`
	SourceType     string          `json:"source_type,omitempty"`
}

type ToolProfileActivateInput struct {
	Tool           string `json:"tool"`
	ProfileID      string `json:"profile_id"`
	ProfileVersion string `json:"profile_version"`
}

type ToolProfileAnalyzeInput struct {
	SamplesPath       string          `json:"samples_path,omitempty"`
	SamplePayload     json.RawMessage `json:"sample_payload,omitempty"`
	BaselineProfileID string          `json:"baseline_profile_id,omitempty"`
}

type ToolProfileCandidate struct {
	SchemaVersion     string          `json:"schema_version"`
	BaselineProfileID string          `json:"baseline_profile_id,omitempty"`
	SourceType        string          `json:"source_type,omitempty"`
	Confidence        string          `json:"confidence"`
	Diagnostics       json.RawMessage `json:"diagnostics"`
	ProfilePayload    json.RawMessage `json:"profile_payload,omitempty"`
	FixturePayload    json.RawMessage `json:"fixture_payload,omitempty"`
}

type ToolMetadata struct {
	Tool                string `json:"tool"`
	ToolVersion         string `json:"tool_version"`
	ProfileID           string `json:"profile_id"`
	ProfileVersion      string `json:"profile_version"`
	CompatibilityStatus string `json:"compatibility_status"`
	CertificationStatus string `json:"certification_status"`
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

type ProjectLinkListOptions struct {
	ListOptions
	ProjectID    string
	RepositoryID string
	LinkType     string
}

type IgnoreRuleListOptions struct {
	ListOptions
	ScopeType string
	ScopeID   string
}

type RepositoryListOptions struct {
	ListOptions
	Provider        string
	ProviderHost    string
	FullPath        string
	Status          string
	DiscoverySource string
	AutoSyncEnabled *bool
}

type WorkspaceListOptions struct {
	ListOptions
	ProjectID     string
	EnvironmentID string
}

type ProjectScanListOptions struct {
	ListOptions
	ProjectID string
	Status    string
}

type FindingListOptions struct {
	ListOptions
	ProjectID     string
	RepositoryID  string
	ProjectScanID string
	Severity      string
	Status        string
}

type RuleSetListOptions struct {
	ListOptions
	Active *bool
}

type ToolProfileListOptions struct {
	ListOptions
	Tool       string
	SourceType string
	Active     *bool
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

type ProjectScanPayload struct {
	SchemaVersion string `json:"schema_version"`
	ProjectID     string `json:"project_id"`
	ProjectScanID string `json:"project_scan_id"`
	ScanType      string `json:"scan_type"`
	RuleSetID     string `json:"rule_set_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type ProjectScanResult struct {
	SchemaVersion               string         `json:"schema_version"`
	ProjectID                   string         `json:"project_id"`
	ProjectScanID               string         `json:"project_scan_id"`
	Tools                       []ToolMetadata `json:"tools"`
	Providers                   []string       `json:"providers"`
	RequiredAuth                []string       `json:"required_auth"`
	CheckResults                []any          `json:"check_results"`
	SecurityValidationRequested bool           `json:"security_validation_requested"`
	SecurityValidationJobID     string         `json:"security_validation_job_id,omitempty"`
}

type SecurityScanPayload struct {
	SchemaVersion  string   `json:"schema_version"`
	ProjectID      string   `json:"project_id"`
	ProjectScanID  string   `json:"project_scan_id"`
	RuleSetID      string   `json:"rule_set_id,omitempty"`
	EnabledModules []string `json:"enabled_modules,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

type SecurityScanResult struct {
	SchemaVersion    string         `json:"schema_version"`
	ProjectID        string         `json:"project_id"`
	ProjectScanID    string         `json:"project_scan_id"`
	Tools            []ToolMetadata `json:"tools"`
	ModulesSucceeded int            `json:"modules_succeeded"`
	ModulesFailed    int            `json:"modules_failed"`
	FindingsCreated  int            `json:"findings_created"`
	FindingsUpdated  int            `json:"findings_updated"`
}

type ProjectScanRef struct {
	ProjectScanID string `json:"project_scan_id"`
	JobID         string `json:"job_id"`
	JobGroupID    string `json:"job_group_id"`
	Status        string `json:"status"`
	SchemaVersion string `json:"schema_version"`
}

type FindingUpsert struct {
	ProjectID     string
	RepositoryID  string
	WorkspaceID   string
	JobID         string
	RuleSetID     string
	CheckType     string
	RuleID        string
	Severity      string
	FilePath      string
	ResourceRef   string
	Title         string
	Description   string
	Remediation   string
	FindingKey    string
	RuleNamespace string
	Tool          string
}

type FindingUpsertResult struct {
	Created bool
	Finding SecurityFinding
}

func marshalJSON(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}
