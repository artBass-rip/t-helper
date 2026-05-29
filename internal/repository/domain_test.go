package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeIdentityGitHubEquivalentURLs(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/example/repo.git",
		"git@github.com:example/repo.git",
		"ssh://git@github.com/example/repo.git",
	} {
		identity, err := NormalizeIdentity(CloneRequest{
			Provider:   ProviderGitHub,
			Protocol:   ProtocolSSH,
			CloneURL:   raw,
			CloneScope: "single_repository",
		}, nil)
		if err != nil {
			t.Fatalf("normalize %q: %v", raw, err)
		}
		if identity.Provider != ProviderGitHub || identity.ProviderHost != "github.com" || identity.FullPath != "example/repo" {
			t.Fatalf("unexpected identity for %q: %+v", raw, identity)
		}
	}
}

func TestNormalizeIdentityUsesSelectedProtocolForManagedTransport(t *testing.T) {
	identity, err := NormalizeIdentity(CloneRequest{
		Provider:   ProviderGitHub,
		Protocol:   ProtocolSSH,
		CloneURL:   "https://github.com/example/repo.git",
		CloneScope: "single_repository",
	}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	if identity.Protocol != ProtocolSSH || identity.CloneURL != "git@github.com:example/repo.git" {
		t.Fatalf("identity = %+v, want ssh transport URL", identity)
	}
}

func TestNormalizeIdentityRejectsUserInfo(t *testing.T) {
	for _, raw := range []string{
		"https://user:token@github.com/example/repo.git",
		"ssh://git:secret@github.com/example/repo.git",
	} {
		_, err := NormalizeIdentity(CloneRequest{
			Provider:   ProviderGitHub,
			Protocol:   ProtocolHTTPS,
			CloneURL:   raw,
			CloneScope: "single_repository",
		}, nil)
		if err == nil {
			t.Fatalf("expected userinfo validation error for %q", raw)
		}
		if code := ValidationCode(err); code != "credential_userinfo_not_allowed" {
			t.Fatalf("validation code for %q = %q", raw, code)
		}
	}
}

func TestNormalizeIdentityPreservesProviderHostPort(t *testing.T) {
	identity, err := NormalizeIdentity(CloneRequest{
		Provider:   ProviderGitHub,
		Protocol:   ProtocolHTTPS,
		CloneURL:   "https://ghe.example.internal:8443/example/repo.git",
		CloneScope: "single_repository",
	}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	if identity.ProviderHost != "ghe.example.internal:8443" {
		t.Fatalf("provider_host = %q, want host:port", identity.ProviderHost)
	}
}

func TestNormalizeIdentityUsesSSHURLFormForNonDefaultPort(t *testing.T) {
	identity, err := NormalizeIdentity(CloneRequest{
		Provider:   ProviderGitHub,
		Protocol:   ProtocolSSH,
		CloneURL:   "ssh://git@ghe.example.internal:2222/example/repo.git",
		CloneScope: "single_repository",
	}, nil)
	if err != nil {
		t.Fatalf("normalize identity: %v", err)
	}
	if identity.ProviderHost != "ghe.example.internal:2222" {
		t.Fatalf("provider_host = %q, want host:port", identity.ProviderHost)
	}
	if identity.CloneURL != "ssh://git@ghe.example.internal:2222/example/repo.git" {
		t.Fatalf("clone_url = %q, want ssh URL with port", identity.CloneURL)
	}
}

func TestNormalizeIdentityDropsDefaultPorts(t *testing.T) {
	for _, tc := range []struct {
		protocol string
		raw      string
		wantHost string
		wantURL  string
	}{
		{ProtocolHTTPS, "https://github.com:443/example/repo.git", "github.com", "https://github.com/example/repo.git"},
		{ProtocolSSH, "ssh://git@github.com:22/example/repo.git", "github.com", "git@github.com:example/repo.git"},
	} {
		identity, err := NormalizeIdentity(CloneRequest{
			Provider:   ProviderGitHub,
			Protocol:   tc.protocol,
			CloneURL:   tc.raw,
			CloneScope: "single_repository",
		}, nil)
		if err != nil {
			t.Fatalf("normalize %q: %v", tc.raw, err)
		}
		if identity.ProviderHost != tc.wantHost || identity.CloneURL != tc.wantURL {
			t.Fatalf("identity for %q = %+v, want host %q clone_url %q", tc.raw, identity, tc.wantHost, tc.wantURL)
		}
	}
}

func TestNormalizeIdentityGenericDefaultsToLocalHost(t *testing.T) {
	identity, err := NormalizeIdentity(CloneRequest{
		Provider:   ProviderGeneric,
		Protocol:   ProtocolHTTPS,
		FullPath:   "repo",
		CloneScope: "single_repository",
	}, nil)
	if err != nil {
		t.Fatalf("normalize generic identity: %v", err)
	}
	if identity.ProviderHost != "local" {
		t.Fatalf("generic provider_host = %q, want local", identity.ProviderHost)
	}
}

func TestNormalizeIdentityGenericLocalCloneURLs(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for _, raw := range []string{source, "file://" + source} {
		identity, err := NormalizeIdentity(CloneRequest{
			Provider:   ProviderGeneric,
			Protocol:   ProtocolHTTPS,
			CloneURL:   raw,
			CloneScope: "single_repository",
		}, nil)
		if err != nil {
			t.Fatalf("normalize %q: %v", raw, err)
		}
		if identity.ProviderHost != "local" || identity.CloneURL != source || identity.FullPath == "" || identity.FullPath[0] == '/' {
			t.Fatalf("unexpected generic local identity for %q: %+v", raw, identity)
		}
	}
}

func TestNormalizeIdentityAcceptsBareHostPath(t *testing.T) {
	identity, err := NormalizeIdentity(CloneRequest{
		Provider:   ProviderGitHub,
		Protocol:   ProtocolHTTPS,
		CloneURL:   "github.com/example/repo",
		CloneScope: "single_repository",
	}, nil)
	if err != nil {
		t.Fatalf("normalize bare host/path: %v", err)
	}
	if identity.ProviderHost != "github.com" || identity.FullPath != "example/repo" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestNormalizeIdentityRejectsExplicitURLMismatch(t *testing.T) {
	_, err := NormalizeIdentity(CloneRequest{
		Provider:     ProviderGitHub,
		ProviderHost: "github.com",
		Protocol:     ProtocolHTTPS,
		CloneURL:     "https://ghe.example.internal/example/repo.git",
		FullPath:     "example/repo",
		CloneScope:   "single_repository",
	}, nil)
	if err == nil {
		t.Fatal("expected provider host mismatch validation error")
	}
}

func TestNormalizeIdentityRejectsUnsupportedProviderFromRegistry(t *testing.T) {
	_, err := NormalizeIdentity(CloneRequest{
		Provider:   "gitlab",
		Protocol:   ProtocolHTTPS,
		FullPath:   "group/repo",
		CloneScope: "single_repository",
	}, nil)
	if err == nil {
		t.Fatal("expected unsupported provider validation error")
	}
	if code := ValidationCode(err); code != "unsupported_provider" {
		t.Fatalf("validation code = %q, want unsupported_provider", code)
	}
}

func TestNormalizeTargetRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, _, err := NormalizeTarget(root, "../escape"); err == nil {
		t.Fatal("expected traversal validation error")
	}
}

func TestNormalizeFullPathRejectsUnsafeSegmentsBeforeCleaning(t *testing.T) {
	for _, value := range []string{"example/../repo", "example\\repo", "example/\x01repo"} {
		_, err := NormalizeFullPath(ProviderGitHub, value)
		if err == nil {
			t.Fatalf("expected validation error for %q", value)
		}
		var validation ValidationError
		if !errors.As(err, &validation) || validation.Code != "invalid_repository_path" {
			t.Fatalf("error for %q = %v, want invalid_repository_path", value, err)
		}
	}
}

func TestNormalizeTargetRejectsBackslash(t *testing.T) {
	root := t.TempDir()
	_, _, err := NormalizeTarget(root, "teams\\repo")
	if err == nil {
		t.Fatal("expected backslash validation error")
	}
	if code := ValidationCode(err); code != "invalid_repository_path" {
		t.Fatalf("validation code = %q", code)
	}
}

func TestNormalizeTargetReturnsContainedPath(t *testing.T) {
	root := t.TempDir()
	target, local, err := NormalizeTarget(root, "teams/repo")
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	if target != "teams/repo" {
		t.Fatalf("target = %q, want teams/repo", target)
	}
	expectedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval root: %v", err)
	}
	if local != filepath.Join(expectedRoot, "teams", "repo") {
		t.Fatalf("local = %q", local)
	}
}

func TestNormalizeTargetCanonicalizesUnicodeNFC(t *testing.T) {
	root := t.TempDir()
	target, _, err := NormalizeTarget(root, "teams/re\u0301po")
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	if target != "teams/répo" {
		t.Fatalf("target = %q, want NFC normalized path", target)
	}
}

func TestNormalizeTargetRejectsSymlinkEscapeInExistingParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, _, err := NormalizeTarget(root, "linked/repo")
	if err == nil {
		t.Fatal("expected symlink escape validation error")
	}
	if code := ValidationCode(err); code != "invalid_repository_path" {
		t.Fatalf("validation code = %q", code)
	}
}
