package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/jobs"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/storage"
)

type Config struct {
	StorageProvider   string
	StorageDSN        string
	LogLevel          string
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
}

func DefaultConfig() Config {
	return Config{
		StorageProvider:   "sqlite",
		StorageDSN:        ".artifacts/dev/sqlite/t-helper.db",
		LogLevel:          "info",
		PollInterval:      time.Second,
		LeaseDuration:     30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
	}
}

type App struct {
	cfg      Config
	registry *storage.Registry
	logger   *slog.Logger
}

func New(cfg Config, registry *storage.Registry, logger *slog.Logger) *App {
	return &App{cfg: cfg, registry: registry, logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	handle, err := a.registry.Open(ctx, storage.Config{Provider: a.cfg.StorageProvider, DSN: a.cfg.StorageDSN})
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
	defer handle.Close()

	configStore := appconfig.NewStore(handle)
	moduleStore := modules.NewStore(handle)
	settings, err := configStore.RuntimeSettings(ctx)
	if err != nil {
		return err
	}
	if err := moduleStore.Seed(ctx, settings.EnabledModules); err != nil {
		return err
	}
	jobStore := jobs.NewStore(handle)
	runtime := jobs.NewRuntime(jobs.RuntimeOptions{
		Store:             jobStore,
		Handlers:          jobs.ModuleHandlers(configStore, moduleStore),
		Logger:            a.logger,
		PollInterval:      a.cfg.PollInterval,
		LeaseDuration:     a.cfg.LeaseDuration,
		HeartbeatInterval: a.cfg.HeartbeatInterval,
	})
	a.logger.Info("thelper-worker started", "provider", handle.Provider)
	return runtime.Run(ctx)
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
