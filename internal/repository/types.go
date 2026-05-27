package repository

import "time"

const (
	ProviderGeneric = "generic"
	ProviderGitHub  = "github"

	ProtocolHTTPS = "https"
	ProtocolSSH   = "ssh"

	UsageGitTransport = "git_transport"
	UsageProviderAPI  = "provider_api"
	UsageWebhook      = "webhook"

	AuthTypeSSHKey        = "ssh_key"
	AuthTypeHTTPSToken    = "https_token"
	AuthTypeHTTPSBasic    = "https_basic"
	AuthTypeOAuthToken    = "oauth_token"
	AuthTypeAppPassword   = "app_password"
	AuthTypeWebhookSecret = "webhook_secret"

	ProviderInstanceSchemaVersion = "repository_provider_instance.v1"
	CredentialSchemaVersion       = "repository_credential.v1"
	RepoClonePayloadSchema        = "jobs.repo_clone.payload.v1"
	RepoPullPayloadSchema         = "jobs.repo_pull.payload.v1"
	RepoSyncPayloadSchema         = "jobs.repo_sync.payload.v1"
	RepoOperationResultSchema     = "jobs.repo_operation.result.v1"
)

type ProviderInstance struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	ProviderHost  string    `json:"provider_host"`
	APIBaseURL    string    `json:"api_base_url,omitempty"`
	WebBaseURL    string    `json:"web_base_url,omitempty"`
	Enabled       bool      `json:"enabled"`
	SchemaVersion string    `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ProviderInstanceInput struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Provider     string `json:"provider"`
	ProviderHost string `json:"provider_host"`
	APIBaseURL   string `json:"api_base_url,omitempty"`
	WebBaseURL   string `json:"web_base_url,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
}

type Credential struct {
	ID                 string    `json:"id"`
	ProviderInstanceID string    `json:"provider_instance_id"`
	Name               string    `json:"name"`
	AuthType           string    `json:"auth_type"`
	SecretRef          string    `json:"secret_ref"`
	Username           string    `json:"username,omitempty"`
	Usages             []string  `json:"usages"`
	ScopeHint          string    `json:"scope_hint,omitempty"`
	Enabled            bool      `json:"enabled"`
	SchemaVersion      string    `json:"schema_version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CredentialInput struct {
	ID                 string   `json:"id,omitempty"`
	ProviderInstanceID string   `json:"provider_instance_id"`
	Name               string   `json:"name"`
	AuthType           string   `json:"auth_type"`
	SecretRef          string   `json:"secret_ref"`
	Username           string   `json:"username,omitempty"`
	Usages             []string `json:"usages"`
	ScopeHint          string   `json:"scope_hint,omitempty"`
	Enabled            *bool    `json:"enabled,omitempty"`
}

type ListOptions struct {
	Limit  int
	Cursor string
}

type ProviderInstanceListOptions struct {
	ListOptions
	Provider     string
	ProviderHost string
	Enabled      *bool
}

type CredentialListOptions struct {
	ListOptions
	ProviderInstanceID string
	Usage              string
	AuthType           string
}

type CloneRequest struct {
	ProviderInstanceID string `json:"provider_instance_id,omitempty"`
	Provider           string `json:"provider"`
	ProviderHost       string `json:"provider_host,omitempty"`
	CredentialID       string `json:"credential_id,omitempty"`
	Protocol           string `json:"protocol"`
	CloneURL           string `json:"clone_url,omitempty"`
	GroupPath          string `json:"group_path,omitempty"`
	CloneScope         string `json:"clone_scope"`
	FullPath           string `json:"full_path,omitempty"`
	RootPathID         string `json:"root_path_id,omitempty"`
	NewRootPath        string `json:"new_root_path,omitempty"`
	TargetDirectory    string `json:"target_directory,omitempty"`
	NewTargetDirectory string `json:"new_target_directory,omitempty"`
}

type PullRequest struct {
	RepositoryID string `json:"repository_id"`
	CredentialID string `json:"credential_id,omitempty"`
}

type SyncRequest struct {
	RepositoryID string `json:"repository_id"`
	CredentialID string `json:"credential_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type RepoClonePayload struct {
	SchemaVersion      string `json:"schema_version"`
	RepositoryID       string `json:"repository_id"`
	ProviderInstanceID string `json:"provider_instance_id,omitempty"`
	Provider           string `json:"provider"`
	ProviderHost       string `json:"provider_host,omitempty"`
	CredentialID       string `json:"credential_id,omitempty"`
	Protocol           string `json:"protocol"`
	CloneURL           string `json:"clone_url,omitempty"`
	CloneScope         string `json:"clone_scope"`
	FullPath           string `json:"full_path"`
	RootPathID         string `json:"root_path_id"`
	TargetDirectory    string `json:"target_directory"`
	LocalPath          string `json:"local_path"`
	RepositoryCreated  bool   `json:"repository_created,omitempty"`
}

type RepoPullPayload struct {
	SchemaVersion string `json:"schema_version"`
	RepositoryID  string `json:"repository_id"`
	CredentialID  string `json:"credential_id,omitempty"`
}

type RepoSyncPayload struct {
	SchemaVersion string `json:"schema_version"`
	RepositoryID  string `json:"repository_id"`
	CredentialID  string `json:"credential_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type OperationResult struct {
	SchemaVersion       string  `json:"schema_version"`
	RepositoryID        string  `json:"repository_id"`
	ProviderInstanceID  string  `json:"provider_instance_id,omitempty"`
	CredentialID        string  `json:"credential_id,omitempty"`
	Operation           string  `json:"operation"`
	RootPathID          string  `json:"root_path_id,omitempty"`
	Provider            string  `json:"provider,omitempty"`
	ProviderHost        string  `json:"provider_host,omitempty"`
	Protocol            string  `json:"protocol,omitempty"`
	LocalPath           string  `json:"local_path,omitempty"`
	RepositoriesCreated int     `json:"repositories_created"`
	BeforeRevision      *string `json:"before_revision"`
	AfterRevision       string  `json:"after_revision,omitempty"`
	Changed             bool    `json:"changed"`
	ExitCode            int     `json:"exit_code"`
}
