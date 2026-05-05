package migrations

import (
	"context"
	"database/sql"
	"embed"
	"sync"

	"github.com/pressly/goose/v3"
)

//go:embed sqlite/*.sql postgres/*.sql
var files embed.FS

var gooseMu sync.Mutex

func Apply(ctx context.Context, db *sql.DB, dialect string) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	goose.SetBaseFS(files)
	if err := goose.SetDialect(dialect); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, dialect)
}
