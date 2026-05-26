package repository

import (
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

func TestNormalizeIdentityRejectsUserInfo(t *testing.T) {
	_, err := NormalizeIdentity(CloneRequest{
		Provider:   ProviderGitHub,
		Protocol:   ProtocolHTTPS,
		CloneURL:   "https://user:token@github.com/example/repo.git",
		CloneScope: "single_repository",
	}, nil)
	if err == nil {
		t.Fatal("expected userinfo validation error")
	}
}

func TestNormalizeTargetRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, _, err := NormalizeTarget(root, "../escape"); err == nil {
		t.Fatal("expected traversal validation error")
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
	if local != filepath.Join(root, "teams", "repo") {
		t.Fatalf("local = %q", local)
	}
}
