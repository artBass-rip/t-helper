package storage

import (
	"context"
	"database/sql"
)

type Config struct {
	Provider string
	DSN      string
}

type Handle struct {
	Provider    string
	DB          *sql.DB
	Fingerprint string
}

func (h *Handle) Close() error {
	if h == nil || h.DB == nil {
		return nil
	}
	return h.DB.Close()
}

type Provider interface {
	Name() string
	Open(ctx context.Context, cfg Config) (*Handle, error)
	Migrate(ctx context.Context, handle *Handle) error
}
