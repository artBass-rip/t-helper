package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/httpapi"
	applog "github.com/artBass-rip/t-helper/internal/log"
	"github.com/artBass-rip/t-helper/internal/modules"
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
	instanceID, err := runtime.NewInstanceID()
	if err != nil {
		return err
	}
	handle, err := a.registry.Open(ctx, storage.Config{
		Provider: a.cfg.StorageProvider,
		DSN:      a.cfg.StorageDSN,
	})
	if err != nil {
		return err
	}

	if err := a.registry.Migrate(ctx, handle); err != nil {
		_ = handle.Close()
		return fmt.Errorf("apply migrations: %w", err)
	}
	handle, err = a.resolveCurrentProfileHandle(ctx, handle)
	if err != nil {
		_ = handle.Close()
		return err
	}
	if err := a.applyPersistedRuntimeConfig(ctx, handle); err != nil {
		_ = handle.Close()
		return err
	}
	defer handle.Close()
	if a.cfg.MigrateOnly {
		a.logger.Info("migrations applied", "provider", handle.Provider)
		return nil
	}

	lock, err := runtime.AcquireLock(a.cfg.RuntimeLockPath, runtime.LockMetadata{
		InstanceID:                instanceID,
		APIListenAddress:          a.cfg.ListenAddress,
		ConfigDatabaseFingerprint: handle.Fingerprint,
	})
	if err != nil {
		return err
	}
	defer lock.Release()

	api, err := a.BuildHandler(ctx, handle, instanceID)
	if err != nil {
		return err
	}
	return a.Serve(ctx, a.cfg.ListenAddress, api)
}

func (a *App) applyPersistedRuntimeConfig(ctx context.Context, handle *storage.Handle) error {
	settings, err := appconfig.NewStore(handle).RuntimeSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Loaded {
		return nil
	}
	if settings.ListenAddress != "" {
		a.cfg.ListenAddress = settings.ListenAddress
	}
	if settings.Mode != "" {
		a.cfg.Mode = settings.Mode
	}
	if settings.LogLevel != "" {
		a.cfg.LogLevel = settings.LogLevel
		a.logger = applog.New(settings.LogLevel)
	}
	return nil
}

func (a *App) resolveCurrentProfileHandle(ctx context.Context, bootstrap *storage.Handle) (*storage.Handle, error) {
	profile, err := appconfig.NewStore(bootstrap).CurrentStorageProfile(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return bootstrap, nil
		}
		return nil, fmt.Errorf("read current storage profile: %w", err)
	}
	if profile.DatabaseFingerprint == bootstrap.Fingerprint {
		return bootstrap, nil
	}
	cfg, err := profile.StorageConfig()
	if err != nil {
		return nil, err
	}
	active, err := a.registry.Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := a.registry.Migrate(ctx, active); err != nil {
		_ = active.Close()
		return nil, fmt.Errorf("apply active profile migrations: %w", err)
	}
	if active.Fingerprint != profile.DatabaseFingerprint {
		_ = active.Close()
		return nil, fmt.Errorf("active storage profile fingerprint mismatch")
	}
	if err := bootstrap.Close(); err != nil {
		_ = active.Close()
		return nil, err
	}
	return active, nil
}

func (a *App) BuildHandler(ctx context.Context, handle *storage.Handle, instanceIDOverride ...string) (http.Handler, error) {
	_ = ctx
	instanceID := ""
	if len(instanceIDOverride) > 0 {
		instanceID = instanceIDOverride[0]
	}
	if instanceID == "" {
		var err error
		instanceID, err = runtime.NewInstanceID()
		if err != nil {
			return nil, err
		}
	}
	health := runtime.NewHealthService(
		instanceID,
		a.cfg.Mode,
		time.Now().UTC(),
		runtime.NewStorageHealthSource(handle),
	)
	configStore := appconfig.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	settings, err := configStore.RuntimeSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err := moduleStore.Seed(ctx, settings.EnabledModules); err != nil {
		return nil, err
	}
	return httpapi.New(
		httpapi.NewHealthHandler(health),
		httpapi.NewConfigHandler(configStore),
		httpapi.NewModulesHandler(configStore, moduleStore),
	), nil
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
