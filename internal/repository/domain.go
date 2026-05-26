package repository

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	ErrValidation          = errors.New("repository validation error")
	ErrNotFound            = errors.New("repository record not found")
	ErrReservationConflict = errors.New("repository operation reservation conflict")
)

type ValidationError struct {
	Code    string
	Message string
}

func (e ValidationError) Error() string {
	if e.Message == "" {
		return ErrValidation.Error() + ": " + e.Code
	}
	return ErrValidation.Error() + ": " + e.Message
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}

type Identity struct {
	Provider     string
	ProviderHost string
	FullPath     string
	CloneURL     string
	Protocol     string
}

func NormalizeIdentity(req CloneRequest, instance *ProviderInstance) (Identity, error) {
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" && instance != nil {
		provider = instance.Provider
	}
	if provider != ProviderGeneric && provider != ProviderGitHub {
		return Identity{}, validationError("unsupported_provider", fmt.Sprintf("unsupported provider %q", provider))
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol != ProtocolHTTPS && protocol != ProtocolSSH {
		return Identity{}, validationError("unsupported_url_protocol", fmt.Sprintf("unsupported protocol %q", protocol))
	}
	host, err := normalizeProviderHost(req.ProviderHost)
	if err != nil {
		return Identity{}, err
	}
	if instance != nil {
		if provider != instance.Provider {
			return Identity{}, validationError("provider_instance_mismatch", "provider_instance_id does not match provider")
		}
		host = instance.ProviderHost
	}
	fullPath := strings.TrimSpace(req.FullPath)
	cloneURL := strings.TrimSpace(req.CloneURL)
	if cloneURL != "" {
		parsed, err := ParseCloneURL(provider, cloneURL)
		if err != nil {
			return Identity{}, err
		}
		if host == "" {
			host = parsed.ProviderHost
		} else if parsed.ProviderHost != "" && host != parsed.ProviderHost {
			return Identity{}, validationError("invalid_provider_host", "provider_host does not match clone_url host")
		}
		if fullPath == "" {
			fullPath = parsed.FullPath
		} else {
			normalizedRequestPath, err := NormalizeFullPath(provider, fullPath)
			if err != nil {
				return Identity{}, err
			}
			if normalizedRequestPath != parsed.FullPath {
				return Identity{}, validationError("provider_path_shape_mismatch", "full_path does not match clone_url path")
			}
		}
		cloneURL = parsed.CloneURL
	}
	if provider == ProviderGeneric && host == "" {
		host = "generic"
	}
	if provider == ProviderGitHub && host == "" {
		host = "github.com"
	}
	if host == "" {
		return Identity{}, validationError("provider_host_required", "provider_host is required")
	}
	fullPath, err = NormalizeFullPath(provider, fullPath)
	if err != nil {
		return Identity{}, err
	}
	if cloneURL == "" {
		cloneURL = TransportURL(provider, host, fullPath, protocol)
	}
	return Identity{Provider: provider, ProviderHost: host, FullPath: fullPath, CloneURL: cloneURL, Protocol: protocol}, nil
}

func ParseCloneURL(provider, raw string) (Identity, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || hasControl(raw) {
		return Identity{}, validationError("unsupported_provider_url", "unsupported provider URL")
	}
	if strings.Contains(raw, "@") && strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Identity{}, validationError("unsupported_provider_url", "unsupported provider URL")
		}
		if u.User != nil && !(u.Scheme == ProtocolSSH && u.User.Username() == "git") {
			return Identity{}, validationError("credential_userinfo_not_allowed", "credential userinfo is not allowed")
		}
	}
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "git@"), ":", 2)
		return identityFromHostPath(provider, parts[0], parts[1], ProtocolSSH)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		if strings.Count(raw, "/") >= 1 && !strings.Contains(raw, "://") && !strings.Contains(raw, "@") {
			parts := strings.SplitN(raw, "/", 2)
			return identityFromHostPath(provider, parts[0], parts[1], ProtocolHTTPS)
		}
		return Identity{}, validationError("unsupported_provider_url", "unsupported provider URL")
	}
	if u.User != nil && !(u.Scheme == ProtocolSSH && u.User.Username() == "git") {
		return Identity{}, validationError("credential_userinfo_not_allowed", "credential userinfo is not allowed")
	}
	if u.Scheme != ProtocolHTTPS && u.Scheme != ProtocolSSH && u.Scheme != "file" {
		return Identity{}, validationError("unsupported_url_protocol", "unsupported URL protocol")
	}
	if u.Scheme == "file" {
		return identityFromHostPath(provider, "local", strings.TrimPrefix(u.Path, "/"), ProtocolHTTPS)
	}
	return identityFromHostPath(provider, u.Host, strings.TrimPrefix(u.Path, "/"), u.Scheme)
}

func identityFromHostPath(provider, host, rawPath, protocol string) (Identity, error) {
	host, err := normalizeProviderHost(host)
	if err != nil {
		return Identity{}, err
	}
	fullPath, err := NormalizeFullPath(provider, rawPath)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Provider: provider, ProviderHost: host, FullPath: fullPath, CloneURL: TransportURL(provider, host, fullPath, protocol), Protocol: protocol}, nil
}

func normalizeProviderHost(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if hasControl(value) || strings.Contains(value, "://") || strings.Contains(value, "/") || strings.Contains(value, "@") || strings.ContainsAny(value, " \t\r\n") {
		return "", validationError("invalid_provider_host", "invalid provider_host")
	}
	host := value
	if strings.Contains(value, ":") {
		parsedHost, port, err := strings.Cut(value, ":")
		if !err || parsedHost == "" || port == "" {
			return "", validationError("invalid_provider_host", "invalid provider_host")
		}
		host = parsedHost
	}
	if host == "." || host == "-" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", validationError("invalid_provider_host", "invalid provider_host")
	}
	return value, nil
}

func NormalizeFullPath(provider, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || hasControl(value) || strings.Contains(value, "\\") {
		return "", validationError("invalid_repository_path", "invalid repository path")
	}
	value = strings.TrimSuffix(value, ".git")
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "", validationError("invalid_repository_path", "invalid repository path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return "", validationError("invalid_repository_path", "invalid repository path")
		}
	}
	if provider == ProviderGitHub && len(strings.Split(value, "/")) != 2 {
		return "", validationError("provider_path_shape_mismatch", "provider path shape mismatch")
	}
	return value, nil
}

func TransportURL(provider, host, fullPath, protocol string) string {
	if protocol == ProtocolSSH {
		return "git@" + host + ":" + fullPath + ".git"
	}
	if provider == ProviderGeneric && host == "local" {
		return fullPath
	}
	return "https://" + host + "/" + fullPath + ".git"
}

func NormalizeTarget(rootPath, target string) (string, string, error) {
	root, err := normalizeAbsPath(rootPath)
	if err != nil {
		return "", "", err
	}
	if evaluatedRoot, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root, err = normalizeAbsPath(evaluatedRoot)
		if err != nil {
			return "", "", err
		}
	} else if !os.IsNotExist(evalErr) {
		return "", "", validationError("invalid_repository_path", "root_path is unavailable")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", validationError("invalid_repository_path", "target_directory is required")
	}
	if hasControl(target) || strings.Contains(target, "\\") {
		return "", "", validationError("invalid_repository_path", "target_directory is invalid")
	}
	target = filepath.Clean(target)
	if filepath.IsAbs(target) {
		return "", "", validationError("invalid_repository_path", "target_directory must be relative")
	}
	slash := filepath.ToSlash(target)
	if slash == "." || strings.HasPrefix(slash, "../") || slash == ".." || strings.Contains(slash, "/../") {
		return "", "", validationError("invalid_repository_path", "target_directory must stay within root_path")
	}
	local, err := containedTargetPath(root, slash)
	if err != nil {
		return "", "", err
	}
	local, err = normalizeAbsPath(local)
	if err != nil {
		return "", "", err
	}
	if !withinRoot(root, local) {
		return "", "", validationError("invalid_repository_path", "target_directory must stay within root_path")
	}
	rel, err := filepath.Rel(root, local)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(rel), local, nil
}

func containedTargetPath(root, slashTarget string) (string, error) {
	current := root
	for _, part := range strings.Split(slashTarget, "/") {
		next := filepath.Join(current, part)
		evaluated, err := filepath.EvalSymlinks(next)
		if err == nil {
			if !withinRoot(root, evaluated) {
				return "", validationError("invalid_repository_path", "target_directory must stay within root_path")
			}
			current = evaluated
			continue
		}
		if !os.IsNotExist(err) {
			return "", validationError("invalid_repository_path", "target_directory is unavailable")
		}
		current = next
		if !withinRoot(root, current) {
			return "", validationError("invalid_repository_path", "target_directory must stay within root_path")
		}
	}
	return current, nil
}

func normalizeAbsPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", validationError("invalid_repository_path", "path is required")
	}
	if !filepath.IsAbs(value) {
		return "", validationError("invalid_repository_path", "path must be absolute")
	}
	cleaned, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", err
	}
	return cleaned, nil
}

func withinRoot(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (!strings.HasPrefix(rel, "../") && rel != "..")
}

func validateSecretRef(value string) error {
	if !regexp.MustCompile(`^secretref://env/[A-Za-z_][A-Za-z0-9_]*$`).MatchString(strings.TrimSpace(value)) {
		return validationError("invalid_secret_ref", "secret_ref must use secretref://env/NAME")
	}
	return nil
}

func validationErrorf(format string, args ...any) error {
	return validationError("validation_error", fmt.Sprintf(format, args...))
}

func validationError(code, message string) error {
	return ValidationError{Code: code, Message: message}
}

func ValidationCode(err error) string {
	var validation ValidationError
	if errors.As(err, &validation) && validation.Code != "" {
		return validation.Code
	}
	return "validation_error"
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
