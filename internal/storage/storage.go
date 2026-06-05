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

func (h *Handle) Dialect() Dialect {
	if h == nil {
		return NewDialect("")
	}
	return NewDialect(h.Provider)
}

type Provider interface {
	Name() string
	Open(ctx context.Context, cfg Config) (*Handle, error)
	Migrate(ctx context.Context, handle *Handle) error
}
