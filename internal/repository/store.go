package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/scanner"
	"github.com/artBass-rip/t-helper/internal/storage"
)

type Store struct {
	handle *storage.Handle
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type ReservationConflictError struct {
	Key string
}

func (e ReservationConflictError) Error() string {
	return ErrReservationConflict.Error() + ": " + e.Key
}

func (e ReservationConflictError) Unwrap() error {
	return ErrReservationConflict
}

func NewStore(handle *storage.Handle) *Store {
	return &Store{handle: handle}
}

func (s *Store) ReserveOperationKeys(ctx context.Context, owner string, ttl time.Duration, keys ...string) ([]string, error) {
	if strings.TrimSpace(owner) == "" {
		owner = newID("reservation_owner")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	now := time.Now().UTC()
	if err := s.expireOperationReservations(ctx, now); err != nil {
		return nil, err
	}
	held := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		query := `INSERT INTO repository_operation_reservations (id, reservation_key, owner, status, created_at, expires_at) VALUES (?, ?, ?, 'held', ?, ?)`
		args := []any{newID("rres"), key, owner, formatTime(now), formatTime(now.Add(ttl))}
		if s.handle.Provider == "postgres" {
			query = `INSERT INTO repository_operation_reservations (id, reservation_key, owner, status, created_at, expires_at) VALUES ($1, $2, $3, 'held', $4, $5)`
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			_ = s.ReleaseOperationReservations(ctx, owner, held...)
			if isUniqueConstraintError(err) {
				return nil, ReservationConflictError{Key: key}
			}
			return nil, err
		}
		held = append(held, key)
	}
	return held, nil
}

func (s *Store) ReleaseOperationReservations(ctx context.Context, owner string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, key := range keys {
		query := `UPDATE repository_operation_reservations SET status = 'released', released_at = ? WHERE reservation_key = ? AND owner = ? AND status = 'held'`
		args := []any{formatTime(now), key, owner}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repository_operation_reservations SET status = 'released', released_at = $1 WHERE reservation_key = $2 AND owner = $3 AND status = 'held'`
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) expireOperationReservations(ctx context.Context, now time.Time) error {
	query := `UPDATE repository_operation_reservations SET status = 'expired' WHERE status = 'held' AND expires_at <= ?`
	args := []any{formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `UPDATE repository_operation_reservations SET status = 'expired' WHERE status = 'held' AND expires_at <= $1`
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) UpsertProviderInstances(ctx context.Context, inputs []ProviderInstanceInput) ([]ProviderInstance, error) {
	out := make([]ProviderInstance, 0, len(inputs))
	for _, input := range inputs {
		item, err := s.upsertProviderInstance(ctx, input)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) upsertProviderInstance(ctx context.Context, input ProviderInstanceInput) (ProviderInstance, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider != ProviderGeneric && provider != ProviderGitHub {
		return ProviderInstance{}, validationErrorf("unsupported provider %q", provider)
	}
	hostProtocol := ""
	if provider == ProviderGitHub {
		hostProtocol = ProtocolHTTPS
	}
	host, err := normalizeProviderHostForProtocol(input.ProviderHost, hostProtocol)
	if err != nil {
		return ProviderInstance{}, err
	}
	if host == "" {
		if provider == ProviderGitHub {
			host = "github.com"
		} else {
			host = "local"
		}
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = provider + ":" + host
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	existing, err := s.findProviderInstance(ctx, input.ID, provider, host)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ProviderInstance{}, err
	}
	if err == nil {
		query := "UPDATE repository_provider_instances SET name = ?, api_base_url = ?, web_base_url = ?, enabled = ?, updated_at = ? WHERE id = ?"
		args := []any{name, nullEmpty(input.APIBaseURL), nullEmpty(input.WebBaseURL), s.boolArg(enabled), formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = "UPDATE repository_provider_instances SET name = $1, api_base_url = $2, web_base_url = $3, enabled = $4, updated_at = $5 WHERE id = $6"
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			return ProviderInstance{}, err
		}
		return s.GetProviderInstance(ctx, existing.ID)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = newID("rpi")
	}
	query := "INSERT INTO repository_provider_instances (id, name, provider, provider_host, api_base_url, web_base_url, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	args := []any{id, name, provider, host, nullEmpty(input.APIBaseURL), nullEmpty(input.WebBaseURL), s.boolArg(enabled), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = "INSERT INTO repository_provider_instances (id, name, provider, provider_host, api_base_url, web_base_url, enabled, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)"
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		if isUniqueConstraintError(err) {
			existing, findErr := s.findProviderInstance(ctx, "", provider, host)
			if findErr != nil {
				return ProviderInstance{}, err
			}
			input.ID = existing.ID
			return s.upsertProviderInstance(ctx, input)
		}
		return ProviderInstance{}, err
	}
	return s.GetProviderInstance(ctx, id)
}

func (s *Store) ListProviderInstances(ctx context.Context, opts ProviderInstanceListOptions) ([]ProviderInstance, error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.Provider != "" {
		add("provider = %s", opts.Provider)
	}
	if opts.ProviderHost != "" {
		add("provider_host = %s", opts.ProviderHost)
	}
	if opts.Enabled != nil {
		add("enabled = %s", s.boolArg(*opts.Enabled))
	}
	query := "SELECT " + s.providerInstanceColumns() + " FROM repository_provider_instances"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT " + s.placeholder(len(args)+1)
	args = append(args, limit(opts.Limit))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderInstance
	for rows.Next() {
		item, err := scanProviderInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetProviderInstance(ctx context.Context, id string) (ProviderInstance, error) {
	query := "SELECT " + s.providerInstanceColumns() + " FROM repository_provider_instances WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.providerInstanceColumns() + " FROM repository_provider_instances WHERE id = $1"
	}
	item, err := scanProviderInstance(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderInstance{}, ErrNotFound
	}
	return item, err
}

func (s *Store) findProviderInstance(ctx context.Context, id, provider, host string) (ProviderInstance, error) {
	if strings.TrimSpace(id) != "" {
		item, err := s.GetProviderInstance(ctx, id)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return item, err
		}
	}
	query := "SELECT " + s.providerInstanceColumns() + " FROM repository_provider_instances WHERE provider = ? AND provider_host = ?"
	args := []any{provider, host}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.providerInstanceColumns() + " FROM repository_provider_instances WHERE provider = $1 AND provider_host = $2"
	}
	item, err := scanProviderInstance(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderInstance{}, ErrNotFound
	}
	return item, err
}

func (s *Store) UpsertCredentials(ctx context.Context, inputs []CredentialInput) ([]Credential, error) {
	out := make([]Credential, 0, len(inputs))
	for _, input := range inputs {
		item, err := s.upsertCredential(ctx, input)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) upsertCredential(ctx context.Context, input CredentialInput) (Credential, error) {
	if _, err := s.GetProviderInstance(ctx, input.ProviderInstanceID); err != nil {
		return Credential{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Credential{}, validationError("validation_error", "name is required")
	}
	authType := strings.ToLower(strings.TrimSpace(input.AuthType))
	if !validCredentialAuthType(authType) {
		return Credential{}, validationError("unsupported_credential_auth_type", fmt.Sprintf("unsupported auth_type %q", authType))
	}
	if err := validateSecretRef(input.SecretRef); err != nil {
		return Credential{}, err
	}
	usages := normalizeUsages(input.Usages)
	if len(usages) == 0 {
		usages = []string{UsageGitTransport}
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	existing, err := s.findCredential(ctx, input.ID, input.ProviderInstanceID, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Credential{}, err
	}
	if err == nil {
		query := "UPDATE repository_credentials SET auth_type = ?, secret_ref = ?, username = ?, usages = ?, scope_hint = ?, enabled = ?, updated_at = ? WHERE id = ?"
		args := []any{authType, input.SecretRef, nullEmpty(input.Username), marshalStrings(usages), nullEmpty(input.ScopeHint), s.boolArg(enabled), formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = "UPDATE repository_credentials SET auth_type = $1, secret_ref = $2, username = $3, usages = $4, scope_hint = $5, enabled = $6, updated_at = $7 WHERE id = $8"
		}
		if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
			return Credential{}, err
		}
		return s.GetCredential(ctx, existing.ID)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = newID("rcred")
	}
	query := "INSERT INTO repository_credentials (id, provider_instance_id, name, auth_type, secret_ref, username, usages, scope_hint, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	args := []any{id, input.ProviderInstanceID, name, authType, input.SecretRef, nullEmpty(input.Username), marshalStrings(usages), nullEmpty(input.ScopeHint), s.boolArg(enabled), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = "INSERT INTO repository_credentials (id, provider_instance_id, name, auth_type, secret_ref, username, usages, scope_hint, enabled, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)"
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return Credential{}, err
	}
	return s.GetCredential(ctx, id)
}

func (s *Store) ListCredentials(ctx context.Context, opts CredentialListOptions) ([]Credential, error) {
	var where []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, s.placeholder(len(args))))
	}
	if opts.ProviderInstanceID != "" {
		add("provider_instance_id = %s", opts.ProviderInstanceID)
	}
	if opts.AuthType != "" {
		add("auth_type = %s", opts.AuthType)
	}
	query := "SELECT " + s.credentialColumns() + " FROM repository_credentials"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT " + s.placeholder(len(args)+1)
	args = append(args, limit(opts.Limit))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		item, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		if opts.Usage != "" && !hasUsage(item.Usages, opts.Usage) {
			continue
		}
		item.SecretRef = maskSecretRef(item.SecretRef)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetCredential(ctx context.Context, id string) (Credential, error) {
	query := "SELECT " + s.credentialColumns() + " FROM repository_credentials WHERE id = ?"
	args := []any{id}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.credentialColumns() + " FROM repository_credentials WHERE id = $1"
	}
	item, err := scanCredential(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ValidateCredential(ctx context.Context, id, providerInstanceID, usage string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	instance, err := s.GetProviderInstance(ctx, providerInstanceID)
	if err != nil {
		return err
	}
	if !instance.Enabled {
		return validationError("provider_instance_disabled", "provider instance is disabled")
	}
	cred, err := s.GetCredential(ctx, id)
	if err != nil {
		return err
	}
	if !cred.Enabled {
		return validationError("credential_disabled", "credential is disabled")
	}
	if cred.ProviderInstanceID != providerInstanceID {
		return validationError("credential_provider_instance_mismatch", "credential does not belong to provider_instance_id")
	}
	if !hasUsage(cred.Usages, usage) {
		return validationError("credential_usage_not_allowed", fmt.Sprintf("credential does not allow %s", usage))
	}
	return nil
}

func (s *Store) findCredential(ctx context.Context, id, providerInstanceID, name string) (Credential, error) {
	if strings.TrimSpace(id) != "" {
		item, err := s.GetCredential(ctx, id)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return item, err
		}
	}
	query := "SELECT " + s.credentialColumns() + " FROM repository_credentials WHERE provider_instance_id = ? AND name = ?"
	args := []any{providerInstanceID, name}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + s.credentialColumns() + " FROM repository_credentials WHERE provider_instance_id = $1 AND name = $2"
	}
	item, err := scanCredential(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	return item, err
}

func (s *Store) UpsertRepository(ctx context.Context, identity Identity, root scanner.RootPath, targetDirectory, localPath, providerInstanceID, credentialID string) (scanner.Repository, error) {
	now := time.Now().UTC()
	name := filepath.Base(identity.FullPath)
	existing, err := s.findRepository(ctx, identity.Provider, identity.ProviderHost, identity.FullPath)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return scanner.Repository{}, err
	}
	existingFound := err == nil
	generic, genericErr := s.findGenericRepositoryByLocalPath(ctx, root.ID, localPath)
	if genericErr != nil && !errors.Is(genericErr, ErrNotFound) {
		return scanner.Repository{}, genericErr
	}
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return scanner.Repository{}, err
	}
	defer tx.Rollback()
	if existingFound {
		if existing.Status == "superseded" || existing.Status == "disabled" {
			return scanner.Repository{}, validationError("repository_status_not_operable", fmt.Sprintf("repository status %q cannot be operated", existing.Status))
		}
		query := `UPDATE repositories SET name = ?, provider_instance_id = ?, clone_url = ?, root_path_id = ?, target_directory = ?, local_path = ?, auth_type = ?, default_credential_id = ?, status = 'active', discovery_source = 'clone', identity_confirmed_at = ?, updated_at = ? WHERE id = ?`
		args := []any{name, nullEmpty(providerInstanceID), identity.CloneURL, root.ID, targetDirectory, localPath, nullEmpty(identity.Protocol), nullEmpty(credentialID), formatTime(now), formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repositories SET name = $1, provider_instance_id = $2, clone_url = $3, root_path_id = $4, target_directory = $5, local_path = $6, auth_type = $7, default_credential_id = $8, status = 'active', discovery_source = 'clone', identity_confirmed_at = $9, updated_at = $10 WHERE id = $11`
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return scanner.Repository{}, err
		}
		if genericErr == nil && generic.ID != existing.ID {
			if err := s.supersedeGenericRepository(ctx, tx, generic.ID, existing.ID, now); err != nil {
				return scanner.Repository{}, err
			}
		}
		if err := tx.Commit(); err != nil {
			return scanner.Repository{}, err
		}
		return scanner.NewStore(s.handle).GetRepository(ctx, existing.ID)
	}
	if genericErr == nil {
		query := `UPDATE repositories SET name = ?, provider_instance_id = ?, provider = ?, provider_host = ?, full_path = ?, clone_url = ?, root_path_id = ?, target_directory = ?, local_path = ?, auth_type = ?, default_credential_id = ?, status = 'active', discovery_source = 'clone', identity_confirmed_at = ?, updated_at = ? WHERE id = ?`
		args := []any{name, nullEmpty(providerInstanceID), identity.Provider, identity.ProviderHost, identity.FullPath, identity.CloneURL, root.ID, targetDirectory, localPath, nullEmpty(identity.Protocol), nullEmpty(credentialID), formatTime(now), formatTime(now), generic.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repositories SET name = $1, provider_instance_id = $2, provider = $3, provider_host = $4, full_path = $5, clone_url = $6, root_path_id = $7, target_directory = $8, local_path = $9, auth_type = $10, default_credential_id = $11, status = 'active', discovery_source = 'clone', identity_confirmed_at = $12, updated_at = $13 WHERE id = $14`
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return scanner.Repository{}, err
		}
		if err := tx.Commit(); err != nil {
			return scanner.Repository{}, err
		}
		return scanner.NewStore(s.handle).GetRepository(ctx, generic.ID)
	}
	id := newID("repo")
	query := `INSERT INTO repositories (id, name, provider_instance_id, provider, provider_host, full_path, clone_url, root_path_id, target_directory, local_path, auth_type, default_credential_id, status, discovery_source, identity_confirmed_at, auto_sync_enabled, webhook_enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 'clone', ?, ?, ?, ?, ?)`
	args := []any{id, name, nullEmpty(providerInstanceID), identity.Provider, identity.ProviderHost, identity.FullPath, identity.CloneURL, root.ID, targetDirectory, localPath, nullEmpty(identity.Protocol), nullEmpty(credentialID), formatTime(now), s.boolArg(false), s.boolArg(false), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO repositories (id, name, provider_instance_id, provider, provider_host, full_path, clone_url, root_path_id, target_directory, local_path, auth_type, default_credential_id, status, discovery_source, identity_confirmed_at, auto_sync_enabled, webhook_enabled, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'active','clone',$13,$14,$15,$16,$17)`
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return scanner.Repository{}, err
	}
	if err := tx.Commit(); err != nil {
		return scanner.Repository{}, err
	}
	return scanner.NewStore(s.handle).GetRepository(ctx, id)
}

func (s *Store) ExistingRepositoryForClone(ctx context.Context, identity Identity, rootPathID, localPath string) (scanner.Repository, error) {
	existing, err := s.findRepository(ctx, identity.Provider, identity.ProviderHost, identity.FullPath)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return existing, err
	}
	generic, err := s.findGenericRepositoryByLocalPath(ctx, rootPathID, localPath)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return generic, err
	}
	return scanner.Repository{}, ErrNotFound
}

func (s *Store) findRepository(ctx context.Context, provider, host, fullPath string) (scanner.Repository, error) {
	query := "SELECT " + repositoryColumns(s.handle.Provider) + " FROM repositories WHERE provider = ? AND provider_host = ? AND full_path = ?"
	args := []any{provider, host, fullPath}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + repositoryColumns(s.handle.Provider) + " FROM repositories WHERE provider = $1 AND provider_host = $2 AND full_path = $3"
	}
	repo, err := scanRepository(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return scanner.Repository{}, ErrNotFound
	}
	return repo, err
}

func (s *Store) findGenericRepositoryByLocalPath(ctx context.Context, rootPathID, localPath string) (scanner.Repository, error) {
	query := "SELECT " + repositoryColumns(s.handle.Provider) + " FROM repositories WHERE provider = ? AND provider_host = ? AND root_path_id = ? AND local_path = ? AND status = 'active'"
	args := []any{ProviderGeneric, "local", rootPathID, localPath}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + repositoryColumns(s.handle.Provider) + " FROM repositories WHERE provider = $1 AND provider_host = $2 AND root_path_id = $3 AND local_path = $4 AND status = 'active'"
	}
	repo, err := scanRepository(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return scanner.Repository{}, ErrNotFound
	}
	return repo, err
}

func (s *Store) supersedeGenericRepository(ctx context.Context, exec sqlExecutor, genericID, canonicalID string, now time.Time) error {
	query := "UPDATE projects SET repository_id = ?, updated_at = ? WHERE repository_id = ?"
	args := []any{canonicalID, formatTime(now), genericID}
	if s.handle.Provider == "postgres" {
		query = "UPDATE projects SET repository_id = $1, updated_at = $2 WHERE repository_id = $3"
	}
	if _, err := exec.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	query = "UPDATE project_links SET repository_id = ?, updated_at = ? WHERE repository_id = ?"
	args = []any{canonicalID, formatTime(now), genericID}
	if s.handle.Provider == "postgres" {
		query = "UPDATE project_links SET repository_id = $1, updated_at = $2 WHERE repository_id = $3"
	}
	if _, err := exec.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	query = "UPDATE repositories SET status = 'superseded', superseded_by_repository_id = ?, updated_at = ? WHERE id = ?"
	args = []any{canonicalID, formatTime(now), genericID}
	if s.handle.Provider == "postgres" {
		query = "UPDATE repositories SET status = 'superseded', superseded_by_repository_id = $1, updated_at = $2 WHERE id = $3"
	}
	_, err := exec.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) TouchRepositoryPulled(ctx context.Context, id string) error {
	query := "UPDATE repositories SET last_pull_at = ?, last_error = NULL, updated_at = ? WHERE id = ?"
	args := []any{formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), id}
	if s.handle.Provider == "postgres" {
		query = "UPDATE repositories SET last_pull_at = $1, last_error = NULL, updated_at = $2 WHERE id = $3"
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) placeholder(idx int) string {
	if s.handle.Provider == "postgres" {
		return fmt.Sprintf("$%d", idx)
	}
	return "?"
}

func (s *Store) boolArg(value bool) any {
	if s.handle.Provider == "postgres" {
		return value
	}
	if value {
		return 1
	}
	return 0
}

func (s *Store) providerInstanceColumns() string {
	if s.handle.Provider == "postgres" {
		return "id, name, provider, provider_host, COALESCE(api_base_url, ''), COALESCE(web_base_url, ''), enabled, created_at::text, updated_at::text"
	}
	return "id, name, provider, provider_host, COALESCE(api_base_url, ''), COALESCE(web_base_url, ''), enabled, created_at, updated_at"
}

func (s *Store) credentialColumns() string {
	if s.handle.Provider == "postgres" {
		return "id, provider_instance_id, name, auth_type, secret_ref, COALESCE(username, ''), usages, COALESCE(scope_hint, ''), enabled, created_at::text, updated_at::text"
	}
	return "id, provider_instance_id, name, auth_type, secret_ref, COALESCE(username, ''), usages, COALESCE(scope_hint, ''), enabled, created_at, updated_at"
}

func scanProviderInstance(row interface{ Scan(dest ...any) error }) (ProviderInstance, error) {
	var item ProviderInstance
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.Provider, &item.ProviderHost, &item.APIBaseURL, &item.WebBaseURL, &item.Enabled, &created, &updated); err != nil {
		return ProviderInstance{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	item.SchemaVersion = ProviderInstanceSchemaVersion
	return item, nil
}

func scanCredential(row interface{ Scan(dest ...any) error }) (Credential, error) {
	var item Credential
	var created, updated, usages string
	if err := row.Scan(&item.ID, &item.ProviderInstanceID, &item.Name, &item.AuthType, &item.SecretRef, &item.Username, &usages, &item.ScopeHint, &item.Enabled, &created, &updated); err != nil {
		return Credential{}, err
	}
	_ = json.Unmarshal([]byte(usages), &item.Usages)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	item.SchemaVersion = CredentialSchemaVersion
	return item, nil
}

func scanRepository(row interface{ Scan(dest ...any) error }) (scanner.Repository, error) {
	var item scanner.Repository
	var identityConfirmed, lastPull, created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.ProviderInstanceID, &item.Provider, &item.ProviderHost, &item.FullPath, &item.CloneURL, &item.DefaultBranch, &item.RootPathID, &item.TargetDirectory, &item.LocalPath, &item.AuthType, &item.DefaultCredentialID, &item.Status, &item.DiscoverySource, &item.SupersededByRepositoryID, &identityConfirmed, &item.AutoSyncEnabled, &item.WebhookEnabled, &item.PollInterval, &lastPull, &item.LastError, &created, &updated); err != nil {
		return scanner.Repository{}, err
	}
	item.IdentityConfirmedAt = parseTimePtr(identityConfirmed)
	item.LastPullAt = parseTimePtr(lastPull)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func repositoryColumns(provider string) string {
	if provider == "postgres" {
		return `id, name, COALESCE(provider_instance_id, ''), provider, provider_host, full_path, COALESCE(clone_url, ''), COALESCE(default_branch, ''), COALESCE(root_path_id, ''), COALESCE(target_directory, ''), COALESCE(local_path, ''), COALESCE(auth_type, ''), COALESCE(default_credential_id, ''), status, discovery_source, COALESCE(superseded_by_repository_id, ''), COALESCE(identity_confirmed_at::text, ''), auto_sync_enabled, webhook_enabled, COALESCE(poll_interval, ''), COALESCE(last_pull_at::text, ''), COALESCE(last_error, ''), created_at::text, updated_at::text`
	}
	return `id, name, COALESCE(provider_instance_id, ''), provider, provider_host, full_path, COALESCE(clone_url, ''), COALESCE(default_branch, ''), COALESCE(root_path_id, ''), COALESCE(target_directory, ''), COALESCE(local_path, ''), COALESCE(auth_type, ''), COALESCE(default_credential_id, ''), status, discovery_source, COALESCE(superseded_by_repository_id, ''), COALESCE(identity_confirmed_at, ''), auto_sync_enabled, webhook_enabled, COALESCE(poll_interval, ''), COALESCE(last_pull_at, ''), COALESCE(last_error, ''), created_at, updated_at`
}

func normalizeUsages(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if value != UsageGitTransport && value != UsageProviderAPI && value != UsageWebhook {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func validCredentialAuthType(value string) bool {
	switch value {
	case AuthTypeSSHKey, AuthTypeHTTPSToken, AuthTypeHTTPSBasic, AuthTypeOAuthToken, AuthTypeAppPassword, AuthTypeWebhookSecret:
		return true
	default:
		return false
	}
}

func hasUsage(values []string, usage string) bool {
	for _, value := range values {
		if value == usage {
			return true
		}
	}
	return false
}

func marshalStrings(values []string) string {
	data, _ := json.Marshal(values)
	return string(data)
}

func maskSecretRef(value string) string {
	if strings.HasPrefix(value, "secretref://env/") {
		return "secretref://env/***"
	}
	return "***"
}

func limit(value int) int {
	if value <= 0 || value > 100 {
		return 100
	}
	return value
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTimePtr(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func nullEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isUniqueConstraintError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

func newID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:])
}
