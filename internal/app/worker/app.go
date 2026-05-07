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
	Concurrency       int
}

func DefaultConfig() Config {
	return Config{
		StorageProvider: "sqlite",
		StorageDSN:      ".artifacts/dev/sqlite/t-helper.db",
		LogLevel:        "info",
		PollInterval:    time.Second,
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
	runtimeOptions, err := a.runtimeOptions(ctx, handle.Provider, configStore, jobStore, moduleStore)
	if err != nil {
		return err
	}
	runtime := jobs.NewRuntime(runtimeOptions)
	a.logger.Info("thelper-worker started", "provider", handle.Provider, "concurrency", runtimeOptions.Concurrency)
	return runtime.Run(ctx)
}

func (a *App) runtimeOptions(ctx context.Context, provider string, configStore *appconfig.Store, jobStore *jobs.Store, moduleStore *modules.Store) (jobs.RuntimeOptions, error) {
	cfg := a.cfg
	defaults := DefaultConfig()
	providerSettings, err := configStore.CurrentWorkerProviderSettings(ctx)
	if err != nil {
		return jobs.RuntimeOptions{}, err
	}
	if cfg.LeaseDuration == 0 || cfg.LeaseDuration == defaults.LeaseDuration {
		cfg.LeaseDuration = providerSettings.LeaseDuration
	}
	if cfg.HeartbeatInterval == 0 || cfg.HeartbeatInterval == defaults.HeartbeatInterval {
		cfg.HeartbeatInterval = providerSettings.HeartbeatInterval
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = providerSettings.WorkersConcurrency
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if provider == "sqlite" && concurrency != 1 {
		return jobs.RuntimeOptions{}, fmt.Errorf("sqlite_worker_concurrency_unsupported")
	}
	return jobs.RuntimeOptions{
		Store:             jobStore,
		Handlers:          jobs.ModuleHandlers(configStore, moduleStore),
		Logger:            a.logger,
		PollInterval:      cfg.PollInterval,
		LeaseDuration:     cfg.LeaseDuration,
		HeartbeatInterval: cfg.HeartbeatInterval,
		Concurrency:       concurrency,
	}, nil
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
