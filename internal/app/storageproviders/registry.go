package storageproviders

import (
	"github.com/artBass-rip/t-helper/internal/storage"
	"github.com/artBass-rip/t-helper/internal/storage/postgres"
	"github.com/artBass-rip/t-helper/internal/storage/sqlite"
)

func MVPRegistry() *storage.Registry {
	return storage.NewRegistry(
		sqlite.NewProvider(),
		postgres.NewProvider(),
	)
}
