package runtime

import (
	"context"

	"github.com/artBass-rip/t-helper/internal/storage"
)

type StorageHealthSource struct {
	handle *storage.Handle
}

func NewStorageHealthSource(handle *storage.Handle) StorageHealthSource {
	return StorageHealthSource{handle: handle}
}

func (s StorageHealthSource) Ping(ctx context.Context) error {
	return s.handle.DB.PingContext(ctx)
}

func (s StorageHealthSource) Fingerprint() string {
	return s.handle.Fingerprint
}
