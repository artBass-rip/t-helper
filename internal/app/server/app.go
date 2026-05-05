package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

	api, err := a.BuildHandler(ctx, handle)
	if err != nil {
		return err
	}
	return a.Serve(ctx, a.cfg.ListenAddress, api)
}

func (a *App) BuildHandler(ctx context.Context, handle *storage.Handle) (http.Handler, error) {
	_ = ctx
	instanceID, err := runtime.NewInstanceID()
	if err != nil {
		return nil, err
	}
	health := runtime.NewHealthService(
		instanceID,
		a.cfg.Mode,
		time.Now().UTC(),
		runtime.NewStorageHealthSource(handle),
	)
	return httpapi.New(httpapi.NewHealthHandler(health)), nil
}

func (a *App) Serve(ctx context.Context, listenAddress string, handler http.Handler) error {
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return err
	}
	return a.ServeListener(ctx, listener, handler)
}

func (a *App) ServeListener(ctx context.Context, listener net.Listener, handler http.Handler) error {
	defer listener.Close()

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("thelper listening", "address", listener.Addr().String(), "mode", a.cfg.Mode)
		errCh <- srv.Serve(listener)
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
