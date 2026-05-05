package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/migrations"
	_ "modernc.org/sqlite"
)

type Provider struct{}

func NewProvider() Provider {
	return Provider{}
}

func (Provider) Name() string {
	return "sqlite"
}

func (Provider) Open(ctx context.Context, cfg storage.Config) (*storage.Handle, error) {
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		return nil, storage.NewValidationError("storage.dsn", "sqlite database path is required")
	}
	path := strings.TrimPrefix(dsn, "file:")
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("prepare sqlite directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if path != ":memory:" {
		if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &storage.Handle{
		Provider:    "sqlite",
		DB:          db,
		Fingerprint: storage.Fingerprint("sqlite", dsn),
	}, nil
}

func (Provider) Migrate(ctx context.Context, handle *storage.Handle) error {
	if handle == nil || handle.DB == nil {
		return storage.NewValidationError("storage.handle", "sqlite storage handle is required")
	}
	return migrations.Apply(ctx, handle.DB, "sqlite")
}
