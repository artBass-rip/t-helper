package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/artBass-rip/t-helper/internal/scanner"
	"github.com/artBass-rip/t-helper/internal/storage"
)

type Store struct {
	handle *storage.Handle
}

const operationReservationRetention = 24 * time.Hour

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type ReservationConflictError struct {
	Key   string
	Owner string
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
			if isUniqueConstraintError(err) {
				existingOwner, ownerErr := s.operationReservationOwner(ctx, key)
				if ownerErr == nil && existingOwner == owner {
					if refreshErr := s.refreshOperationReservation(ctx, owner, ttl, key); refreshErr != nil {
						_ = s.ReleaseOperationReservations(ctx, owner, held...)
						return nil, refreshErr
					}
					held = append(held, key)
					continue
				}
				_ = s.ReleaseOperationReservations(ctx, owner, held...)
				return nil, ReservationConflictError{Key: key, Owner: existingOwner}
			}
			_ = s.ReleaseOperationReservations(ctx, owner, held...)
			return nil, err
		}
		held = append(held, key)
	}
	return held, nil
}

func (s *Store) CleanupOperationReservations(ctx context.Context) error {
	return s.expireOperationReservations(ctx, time.Now().UTC())
}

func (s *Store) TransferOperationReservations(ctx context.Context, fromOwner, toOwner string, ttl time.Duration, keys ...string) error {
	fromOwner = strings.TrimSpace(fromOwner)
	toOwner = strings.TrimSpace(toOwner)
	if fromOwner == "" || toOwner == "" {
		return validationError("validation_error", "reservation owners are required")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	expiresAt := formatTime(time.Now().UTC().Add(ttl))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		query := `UPDATE repository_operation_reservations SET owner = ?, expires_at = ? WHERE reservation_key = ? AND owner = ? AND status = 'held'`
		args := []any{toOwner, expiresAt, key, fromOwner}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repository_operation_reservations SET owner = $1, expires_at = $2 WHERE reservation_key = $3 AND owner = $4 AND status = 'held'`
		}
		result, err := s.handle.DB.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if rows, _ := result.RowsAffected(); rows == 0 {
			return ReservationConflictError{Key: key}
		}
	}
	return nil
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

func (s *Store) operationReservationOwner(ctx context.Context, key string) (string, error) {
	query := `SELECT owner FROM repository_operation_reservations WHERE reservation_key = ? AND status = 'held' LIMIT 1`
	args := []any{key}
	if s.handle.Provider == "postgres" {
		query = `SELECT owner FROM repository_operation_reservations WHERE reservation_key = $1 AND status = 'held' LIMIT 1`
	}
	var owner string
	if err := s.handle.DB.QueryRowContext(ctx, query, args...).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return owner, nil
}

func (s *Store) refreshOperationReservation(ctx context.Context, owner string, ttl time.Duration, key string) error {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	query := `UPDATE repository_operation_reservations SET expires_at = ? WHERE reservation_key = ? AND owner = ? AND status = 'held'`
	args := []any{formatTime(time.Now().UTC().Add(ttl)), key, owner}
	if s.handle.Provider == "postgres" {
		query = `UPDATE repository_operation_reservations SET expires_at = $1 WHERE reservation_key = $2 AND owner = $3 AND status = 'held'`
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) expireOperationReservations(ctx context.Context, now time.Time) error {
	query := `UPDATE repository_operation_reservations SET status = 'expired' WHERE status = 'held' AND expires_at <= ?`
	args := []any{formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `UPDATE repository_operation_reservations SET status = 'expired' WHERE status = 'held' AND expires_at <= $1`
	}
	if _, err := s.handle.DB.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return s.pruneOperationReservations(ctx, now.Add(-operationReservationRetention))
}

func (s *Store) pruneOperationReservations(ctx context.Context, olderThan time.Time) error {
	query := `DELETE FROM repository_operation_reservations WHERE status IN ('released', 'expired') AND COALESCE(released_at, expires_at, created_at) < ?`
	args := []any{formatTime(olderThan)}
	if s.handle.Provider == "postgres" {
		query = `DELETE FROM repository_operation_reservations WHERE status IN ('released', 'expired') AND COALESCE(released_at, expires_at, created_at) < $1`
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
	adapter, err := adapterForProvider(provider)
	if err != nil {
		return ProviderInstance{}, err
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
		host = adapter.defaultHost
	}
	apiBaseURL, err := normalizeProviderProfileURL(input.APIBaseURL, host)
	if err != nil {
		return ProviderInstance{}, err
	}
	webBaseURL, err := normalizeProviderProfileURL(input.WebBaseURL, host)
	if err != nil {
		return ProviderInstance{}, err
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
		args := []any{name, nullEmpty(apiBaseURL), nullEmpty(webBaseURL), s.boolArg(enabled), formatTime(now), existing.ID}
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
	args := []any{id, name, provider, host, nullEmpty(apiBaseURL), nullEmpty(webBaseURL), s.boolArg(enabled), formatTime(now), formatTime(now)}
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

func normalizeProviderProfileURL(value, providerHost string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if hasControl(value) {
		return "", validationError("invalid_provider_profile_url", "provider profile URL is invalid")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", validationError("invalid_provider_profile_url", "provider profile URL is invalid")
	}
	if parsed.Scheme != ProtocolHTTPS {
		return "", validationError("invalid_provider_profile_url", "provider profile URL must use https")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", validationError("invalid_provider_profile_url", "provider profile URL must not include query or fragment")
	}
	if parsed.User != nil {
		return "", validationError("credential_userinfo_not_allowed", "credential userinfo is not allowed")
	}
	host, err := normalizeProviderHostForProtocol(parsed.Host, ProtocolHTTPS)
	if err != nil {
		return "", err
	}
	if host != providerHost {
		return "", validationError("invalid_provider_profile_url", "provider profile URL host must match provider_host")
	}
	parsed.Scheme = ProtocolHTTPS
	parsed.Host = host
	parsed.User = nil
	return parsed.String(), nil
}

func (s *Store) ListProviderInstances(ctx context.Context, opts ProviderInstanceListOptions) ([]ProviderInstance, error) {
	page, err := s.ListProviderInstancesPage(ctx, opts)
	return page.Items, err
}

func (s *Store) ListProviderInstancesPage(ctx context.Context, opts ProviderInstanceListOptions) (Page[ProviderInstance], error) {
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
	return listPage(ctx, s, query, args, opts.ListOptions, scanProviderInstance, func(item ProviderInstance) time.Time {
		return item.CreatedAt
	}, func(item ProviderInstance) string {
		return item.ID
	})
}

func listPage[T any](ctx context.Context, s *Store, baseQuery string, args []any, opts ListOptions, scan func(interface{ Scan(dest ...any) error }) (T, error), itemTime func(T) time.Time, itemID func(T) string) (Page[T], error) {
	limitValue := limit(opts.Limit)
	query := baseQuery
	if opts.Cursor != "" {
		cursor, err := decodeCursor(opts.Cursor)
		if err != nil {
			return Page[T]{}, err
		}
		clause := fmt.Sprintf("(created_at < %s OR (created_at = %s AND id < %s))", s.placeholder(len(args)+1), s.placeholder(len(args)+2), s.placeholder(len(args)+3))
		if strings.Contains(strings.ToUpper(query), " WHERE ") {
			query += " AND " + clause
		} else {
			query += " WHERE " + clause
		}
		args = append(args, formatTime(cursor.Time), formatTime(cursor.Time), cursor.ID)
	}
	args = append(args, limitValue+1)
	query += " ORDER BY created_at DESC, id DESC LIMIT " + s.placeholder(len(args))
	rows, err := s.handle.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[T]{}, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return Page[T]{}, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return Page[T]{}, err
	}
	var next string
	if len(out) > limitValue {
		out = out[:limitValue]
		last := out[len(out)-1]
		next = encodeCursor(itemTime(last), itemID(last))
	}
	return Page[T]{Items: out, NextCursor: next}, nil
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
	usages, err := normalizeUsages(input.Usages)
	if err != nil {
		return Credential{}, err
	}
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
	page, err := s.ListCredentialsPage(ctx, opts)
	return page.Items, err
}

func (s *Store) ListCredentialsPage(ctx context.Context, opts CredentialListOptions) (Page[Credential], error) {
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
	if opts.Usage != "" {
		if opts.Usage != UsageGitTransport && opts.Usage != UsageProviderAPI && opts.Usage != UsageWebhook {
			return Page[Credential]{}, validationError("credential_usage_not_allowed", "unsupported credential usage")
		}
		add("usages LIKE %s", "%\""+opts.Usage+"\"%")
	}
	query := "SELECT " + s.credentialColumns() + " FROM repository_credentials"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	page, err := listPage(ctx, s, query, args, opts.ListOptions, scanCredential, func(item Credential) time.Time {
		return item.CreatedAt
	}, func(item Credential) string {
		return item.ID
	})
	if err != nil {
		return Page[Credential]{}, err
	}
	for idx := range page.Items {
		item := page.Items[idx]
		item.SecretRef = maskSecretRef(item.SecretRef)
		page.Items[idx] = item
	}
	return page, nil
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

func (s *Store) ValidateCredentialForProtocol(ctx context.Context, id, providerInstanceID, usage, protocol string) error {
	if err := s.ValidateCredential(ctx, id, providerInstanceID, usage); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return nil
	}
	cred, err := s.GetCredential(ctx, id)
	if err != nil {
		return err
	}
	protocol = normalizeTransportProtocol(protocol)
	switch protocol {
	case ProtocolHTTPS:
		switch cred.AuthType {
		case AuthTypeHTTPSToken, AuthTypeHTTPSBasic, AuthTypeOAuthToken, AuthTypeAppPassword:
			return nil
		}
	case ProtocolSSH:
		if cred.AuthType == AuthTypeSSHKey {
			return nil
		}
	}
	return validationError("credential_auth_type_protocol_mismatch", fmt.Sprintf("credential auth_type %q is not compatible with %s transport", cred.AuthType, protocol))
}

func normalizeTransportProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "token" {
		return ProtocolHTTPS
	}
	return value
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
	repo, _, err := s.UpsertRepositoryForClone(ctx, identity, root, targetDirectory, localPath, providerInstanceID, credentialID)
	return repo, err
}

func (s *Store) UpsertRepositoryForClone(ctx context.Context, identity Identity, root scanner.RootPath, targetDirectory, localPath, providerInstanceID, credentialID string) (scanner.Repository, bool, error) {
	if err := s.validateRepositoryOperationReferences(ctx, identity, providerInstanceID, credentialID); err != nil {
		return scanner.Repository{}, false, err
	}
	now := time.Now().UTC()
	name := filepath.Base(identity.FullPath)
	existing, err := s.findRepository(ctx, identity.Provider, identity.ProviderHost, identity.FullPath)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return scanner.Repository{}, false, err
	}
	existingFound := err == nil
	generic, genericErr := s.findGenericRepositoryByLocalPath(ctx, root.ID, localPath)
	if genericErr != nil && !errors.Is(genericErr, ErrNotFound) {
		return scanner.Repository{}, false, genericErr
	}
	tx, err := s.handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return scanner.Repository{}, false, err
	}
	defer tx.Rollback()
	if existingFound {
		if existing.Status == "superseded" || existing.Status == "disabled" {
			return scanner.Repository{}, false, validationError("repository_status_not_operable", fmt.Sprintf("repository status %q cannot be operated", existing.Status))
		}
		query := `UPDATE repositories SET name = ?, provider_instance_id = ?, clone_url = ?, root_path_id = ?, target_directory = ?, local_path = ?, auth_type = ?, default_credential_id = ?, status = 'active', discovery_source = 'clone', identity_confirmed_at = ?, updated_at = ? WHERE id = ?`
		args := []any{name, nullEmpty(providerInstanceID), identity.CloneURL, root.ID, targetDirectory, localPath, nullEmpty(identity.Protocol), nullEmpty(credentialID), formatTime(now), formatTime(now), existing.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repositories SET name = $1, provider_instance_id = $2, clone_url = $3, root_path_id = $4, target_directory = $5, local_path = $6, auth_type = $7, default_credential_id = $8, status = 'active', discovery_source = 'clone', identity_confirmed_at = $9, updated_at = $10 WHERE id = $11`
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return scanner.Repository{}, false, err
		}
		if genericErr == nil && generic.ID != existing.ID {
			if err := s.supersedeGenericRepository(ctx, tx, generic.ID, existing.ID, now); err != nil {
				return scanner.Repository{}, false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return scanner.Repository{}, false, err
		}
		repo, err := scanner.NewStore(s.handle).GetRepository(ctx, existing.ID)
		return repo, false, err
	}
	if genericErr == nil {
		query := `UPDATE repositories SET name = ?, provider_instance_id = ?, provider = ?, provider_host = ?, full_path = ?, clone_url = ?, root_path_id = ?, target_directory = ?, local_path = ?, auth_type = ?, default_credential_id = ?, status = 'active', discovery_source = 'clone', identity_confirmed_at = ?, updated_at = ? WHERE id = ?`
		args := []any{name, nullEmpty(providerInstanceID), identity.Provider, identity.ProviderHost, identity.FullPath, identity.CloneURL, root.ID, targetDirectory, localPath, nullEmpty(identity.Protocol), nullEmpty(credentialID), formatTime(now), formatTime(now), generic.ID}
		if s.handle.Provider == "postgres" {
			query = `UPDATE repositories SET name = $1, provider_instance_id = $2, provider = $3, provider_host = $4, full_path = $5, clone_url = $6, root_path_id = $7, target_directory = $8, local_path = $9, auth_type = $10, default_credential_id = $11, status = 'active', discovery_source = 'clone', identity_confirmed_at = $12, updated_at = $13 WHERE id = $14`
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return scanner.Repository{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return scanner.Repository{}, false, err
		}
		repo, err := scanner.NewStore(s.handle).GetRepository(ctx, generic.ID)
		return repo, false, err
	}
	id := newID("repo")
	query := `INSERT INTO repositories (id, name, provider_instance_id, provider, provider_host, full_path, clone_url, root_path_id, target_directory, local_path, auth_type, default_credential_id, status, discovery_source, identity_confirmed_at, auto_sync_enabled, webhook_enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 'clone', ?, ?, ?, ?, ?)`
	args := []any{id, name, nullEmpty(providerInstanceID), identity.Provider, identity.ProviderHost, identity.FullPath, identity.CloneURL, root.ID, targetDirectory, localPath, nullEmpty(identity.Protocol), nullEmpty(credentialID), formatTime(now), s.boolArg(false), s.boolArg(false), formatTime(now), formatTime(now)}
	if s.handle.Provider == "postgres" {
		query = `INSERT INTO repositories (id, name, provider_instance_id, provider, provider_host, full_path, clone_url, root_path_id, target_directory, local_path, auth_type, default_credential_id, status, discovery_source, identity_confirmed_at, auto_sync_enabled, webhook_enabled, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'active','clone',$13,$14,$15,$16,$17)`
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return scanner.Repository{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return scanner.Repository{}, false, err
	}
	repo, err := scanner.NewStore(s.handle).GetRepository(ctx, id)
	return repo, true, err
}

func (s *Store) validateRepositoryOperationReferences(ctx context.Context, identity Identity, providerInstanceID, credentialID string) error {
	providerInstanceID = strings.TrimSpace(providerInstanceID)
	credentialID = strings.TrimSpace(credentialID)
	if providerInstanceID == "" {
		if credentialID != "" {
			return validationError("credential_provider_instance_required", "provider_instance_id is required when credential_id is set")
		}
		return nil
	}
	instance, err := s.GetProviderInstance(ctx, providerInstanceID)
	if err != nil {
		return err
	}
	if !instance.Enabled {
		return validationError("provider_instance_disabled", "provider instance is disabled")
	}
	if instance.Provider != identity.Provider || instance.ProviderHost != identity.ProviderHost {
		return validationError("provider_instance_mismatch", "provider_instance_id does not match repository identity")
	}
	if credentialID != "" {
		if err := s.ValidateCredentialForProtocol(ctx, credentialID, providerInstanceID, UsageGitTransport, identity.Protocol); err != nil {
			return err
		}
	}
	return nil
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

func (s *Store) ActiveRepositoryByLocalTarget(ctx context.Context, rootPathID, targetDirectory, localPath string) (scanner.Repository, error) {
	query := "SELECT " + repositoryColumns(s.handle.Provider) + " FROM repositories WHERE root_path_id = ? AND status = 'active' AND (target_directory = ? OR local_path = ?) ORDER BY updated_at DESC, id DESC LIMIT 1"
	args := []any{rootPathID, targetDirectory, localPath}
	if s.handle.Provider == "postgres" {
		query = "SELECT " + repositoryColumns(s.handle.Provider) + " FROM repositories WHERE root_path_id = $1 AND status = 'active' AND (target_directory = $2 OR local_path = $3) ORDER BY updated_at DESC, id DESC LIMIT 1"
	}
	repo, err := scanRepository(s.handle.DB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return scanner.Repository{}, ErrNotFound
	}
	return repo, err
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

func (s *Store) TouchRepositoryPulledWithBranch(ctx context.Context, id, defaultBranch string) error {
	now := time.Now().UTC()
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch == "" {
		return s.TouchRepositoryPulled(ctx, id)
	}
	query := "UPDATE repositories SET default_branch = ?, last_pull_at = ?, last_error = NULL, updated_at = ? WHERE id = ?"
	args := []any{defaultBranch, formatTime(now), formatTime(now), id}
	if s.handle.Provider == "postgres" {
		query = "UPDATE repositories SET default_branch = $1, last_pull_at = $2, last_error = NULL, updated_at = $3 WHERE id = $4"
	}
	_, err := s.handle.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) MarkRepositoryError(ctx context.Context, id, message string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	query := "UPDATE repositories SET last_error = ?, updated_at = ? WHERE id = ?"
	args := []any{strings.TrimSpace(message), formatTime(time.Now().UTC()), id}
	if s.handle.Provider == "postgres" {
		query = "UPDATE repositories SET last_error = $1, updated_at = $2 WHERE id = $3"
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

func normalizeUsages(values []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if value != UsageGitTransport && value != UsageProviderAPI && value != UsageWebhook {
			return nil, validationError("credential_usage_not_allowed", "unsupported credential usage")
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, nil
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

type listCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func encodeCursor(t time.Time, id string) string {
	data, _ := json.Marshal(listCursor{Time: t.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(value string) (listCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	var cursor listCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	if cursor.Time.IsZero() || cursor.ID == "" {
		return listCursor{}, ErrInvalidCursor
	}
	return cursor, nil
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
