package storage

import (
	"context"
	"errors"
	"testing"
)

func TestRegistryRejectsUnknownProvider(t *testing.T) {
	registry := NewRegistry()
	_, err := registry.Open(context.Background(), Config{Provider: "unknown", DSN: "unused"})
	if err == nil {
		t.Fatal("expected error")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

type stubProvider struct {
	name string
}

func (p stubProvider) Name() string {
	return p.name
}

func (p stubProvider) Open(context.Context, Config) (*Handle, error) {
	return &Handle{Provider: normalizeProvider(p.name), Fingerprint: "db:test"}, nil
}

func (p stubProvider) Migrate(context.Context, *Handle) error {
	return nil
}

func TestRegistryNormalizesPostgreSQLAlias(t *testing.T) {
	registry := NewRegistry(stubProvider{name: "postgres"})
	handle, err := registry.Open(context.Background(), Config{Provider: "postgresql", DSN: "unused"})
	if err != nil {
		t.Fatalf("open postgresql alias: %v", err)
	}
	if handle.Provider != "postgres" {
		t.Fatalf("provider = %q, want postgres", handle.Provider)
	}
}
