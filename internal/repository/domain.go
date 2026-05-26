package repository

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrValidation = errors.New("repository validation error")
	ErrNotFound   = errors.New("repository record not found")
)

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
		return Identity{}, validationErrorf("unsupported provider %q", provider)
	}
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	if protocol != ProtocolHTTPS && protocol != ProtocolSSH {
		return Identity{}, validationErrorf("unsupported protocol %q", protocol)
	}
	host := strings.ToLower(strings.TrimSpace(req.ProviderHost))
	if instance != nil {
		if provider != instance.Provider {
			return Identity{}, validationErrorf("provider_instance_id does not match provider")
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
		}
		if fullPath == "" {
			fullPath = parsed.FullPath
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
		return Identity{}, validationErrorf("provider_host is required")
	}
	fullPath, err := NormalizeFullPath(provider, fullPath)
	if err != nil {
		return Identity{}, err
	}
	if cloneURL == "" {
		cloneURL = TransportURL(provider, host, fullPath, protocol)
	}
	return Identity{Provider: provider, ProviderHost: host, FullPath: fullPath, CloneURL: cloneURL, Protocol: protocol}, nil
}

func ParseCloneURL(provider, raw string) (Identity, error) {
	if strings.Contains(raw, "@") && strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Identity{}, validationErrorf("unsupported_provider_url")
		}
		if u.User != nil && !(u.Scheme == ProtocolSSH && u.User.Username() == "git") {
			return Identity{}, validationErrorf("credential_userinfo_not_allowed")
		}
	}
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "git@"), ":", 2)
		return identityFromHostPath(provider, parts[0], parts[1], ProtocolSSH)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return Identity{}, validationErrorf("unsupported_provider_url")
	}
	if u.User != nil && !(u.Scheme == ProtocolSSH && u.User.Username() == "git") {
		return Identity{}, validationErrorf("credential_userinfo_not_allowed")
	}
	if u.Scheme != ProtocolHTTPS && u.Scheme != ProtocolSSH && u.Scheme != "file" {
		return Identity{}, validationErrorf("unsupported_url_protocol")
	}
	if u.Scheme == "file" {
		return identityFromHostPath(provider, "local", strings.TrimPrefix(u.Path, "/"), ProtocolHTTPS)
	}
	return identityFromHostPath(provider, u.Hostname(), strings.TrimPrefix(u.Path, "/"), u.Scheme)
}

func identityFromHostPath(provider, host, rawPath, protocol string) (Identity, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	fullPath, err := NormalizeFullPath(provider, rawPath)
	if err != nil {
		return Identity{}, err
	}
	return Identity{Provider: provider, ProviderHost: host, FullPath: fullPath, CloneURL: TransportURL(provider, host, fullPath, protocol), Protocol: protocol}, nil
}

func NormalizeFullPath(provider, value string) (string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, ".git"))
	value = path.Clean(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	if value == "." || value == "" || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") {
		return "", validationErrorf("invalid_repository_path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return "", validationErrorf("invalid_repository_path")
		}
	}
	if provider == ProviderGitHub && len(strings.Split(value, "/")) != 2 {
		return "", validationErrorf("provider_path_shape_mismatch")
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
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", validationErrorf("target_directory is required")
	}
	target = filepath.Clean(target)
	if filepath.IsAbs(target) {
		return "", "", validationErrorf("target_directory must be relative")
	}
	slash := filepath.ToSlash(target)
	if slash == "." || strings.HasPrefix(slash, "../") || slash == ".." || strings.Contains(slash, "/../") {
		return "", "", validationErrorf("target_directory must stay within root_path")
	}
	local := filepath.Join(root, filepath.FromSlash(slash))
	evaluated, err := filepath.EvalSymlinks(local)
	if err == nil {
		local = evaluated
	}
	local, err = normalizeAbsPath(local)
	if err != nil {
		return "", "", err
	}
	if !withinRoot(root, local) {
		return "", "", validationErrorf("target_directory must stay within root_path")
	}
	rel, err := filepath.Rel(root, local)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(rel), local, nil
}

func normalizeAbsPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", validationErrorf("path is required")
	}
	if !filepath.IsAbs(value) {
		return "", validationErrorf("path must be absolute")
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
		return validationErrorf("secret_ref must use secretref://env/NAME")
	}
	return nil
}

func validationErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}
