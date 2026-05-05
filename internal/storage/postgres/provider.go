package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Provider struct{}

func NewProvider() Provider {
	return Provider{}
}

func (Provider) Name() string {
	return "postgres"
}

func (Provider) Open(ctx context.Context, cfg storage.Config) (*storage.Handle, error) {
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		return nil, storage.NewValidationError("storage.dsn", "postgres DSN is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &storage.Handle{
		Provider:    "postgres",
		DB:          db,
		Fingerprint: storage.Fingerprint("postgres", dsn),
	}, nil
}

func (Provider) Migrate(ctx context.Context, handle *storage.Handle) error {
	if handle == nil || handle.DB == nil {
		return storage.NewValidationError("storage.handle", "postgres storage handle is required")
	}
	return migrations.Apply(ctx, handle.DB, "postgres")
}
