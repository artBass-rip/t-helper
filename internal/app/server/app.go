package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/artBass-rip/t-helper/internal/httpapi"
	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/artBass-rip/t-helper/internal/storage"
)

type App struct {
	cfg      Config
	registry *storage.Registry
	logger   *slog.Logger
}

func New(cfg Config, registry *storage.Registry, logger *slog.Logger) *App {
	return &App{cfg: cfg, registry: registry, logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	handle, err := a.registry.Open(ctx, storage.Config{
		Provider: a.cfg.StorageProvider,
		DSN:      a.cfg.StorageDSN,
	})
	if err != nil {
		return err
	}
	defer handle.Close()

	if err := a.registry.Migrate(ctx, handle); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if a.cfg.MigrateOnly {
		a.logger.Info("migrations applied", "provider", handle.Provider)
		return nil
	}

	instanceID, err := runtime.NewInstanceID()
	if err != nil {
		return err
	}
	health := runtime.NewHealthService(
		instanceID,
		a.cfg.Mode,
		time.Now().UTC(),
		runtime.NewStorageHealthSource(handle),
	)
	api := httpapi.New(httpapi.NewHealthHandler(health))
	srv := &http.Server{
		Addr:              a.cfg.ListenAddress,
		Handler:           api,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("thelper listening", "address", a.cfg.ListenAddress, "mode", a.cfg.Mode, "provider", handle.Provider)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
