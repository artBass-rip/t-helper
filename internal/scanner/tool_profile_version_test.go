package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionMatchesConstraint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		version    string
		constraint string
		want       bool
	}{
		{name: "minimum exact", version: "1.15.0", constraint: ">=1.15.0", want: true},
		{name: "minimum patch", version: "1.15.3", constraint: ">=1.15.0", want: true},
		{name: "future minor", version: "1.20.0", constraint: ">=1.15.0", want: true},
		{name: "future major", version: "2.0.0", constraint: ">=1.15.0", want: true},
		{name: "below minimum", version: "1.14.9", constraint: ">=1.15.0", want: false},
		{name: "prerelease rejected", version: "1.15.0-alpha.1", constraint: ">=1.15.0", want: false},
		{name: "numeric not lexical", version: "1.9.9", constraint: ">=1.15.0", want: false},
		{name: "major family", version: "1.15.3", constraint: "1", want: true},
		{name: "exact patch", version: "0.60.0", constraint: "0.60.0", want: true},
		{name: "patch boundary", version: "0.60.01", constraint: "0.60.0", want: false},
		{name: "invalid", version: "development", constraint: ">=1.15.0", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := versionMatchesConstraint(test.version, test.constraint); got != test.want {
				t.Fatalf("versionMatchesConstraint(%q, %q) = %v, want %v", test.version, test.constraint, got, test.want)
			}
		})
	}
}

func TestResolveToolExecutableFromConfiguredDirectory(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "custom-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("THELPER_TOOLCHAIN_DIR", dir)
	resolved, err := resolveToolExecutable("custom-tool")
	if err != nil {
		t.Fatalf("resolve configured tool: %v", err)
	}
	if resolved != tool {
		t.Fatalf("resolved tool = %q, want %q", resolved, tool)
	}
}
