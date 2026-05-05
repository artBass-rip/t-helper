package storage

import (
	"context"
	"sort"
	"strings"
)

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		r.providers[normalizeProvider(provider.Name())] = provider
	}
	return r
}

func (r *Registry) Open(ctx context.Context, cfg Config) (*Handle, error) {
	providerName := normalizeProvider(cfg.Provider)
	if providerName == "" {
		return nil, NewValidationError("storage.provider", "storage provider is required")
	}
	provider, ok := r.providers[providerName]
	if !ok {
		return nil, NewValidationError("storage.provider", "unknown storage provider")
	}
	return provider.Open(ctx, Config{
		Provider: providerName,
		DSN:      cfg.DSN,
	})
}

func (r *Registry) Migrate(ctx context.Context, handle *Handle) error {
	if handle == nil {
		return NewValidationError("storage.handle", "storage handle is required")
	}
	provider, ok := r.providers[normalizeProvider(handle.Provider)]
	if !ok {
		return NewValidationError("storage.provider", "unknown storage provider")
	}
	return provider.Migrate(ctx, handle)
}

func (r *Registry) Providers() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "postgresql":
		return "postgres"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}
