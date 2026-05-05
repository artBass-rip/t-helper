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
