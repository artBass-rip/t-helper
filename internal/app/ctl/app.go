package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/artBass-rip/t-helper/internal/storage"
)

type App struct {
	out      io.Writer
	registry *storage.Registry
}

type Command struct {
	Name            string
	StorageProvider string
	StorageDSN      string
	ConfigPath      string
	IgnorePath      string
	ModuleName      string
}

func New(out io.Writer, registry *storage.Registry) *App {
	return &App{out: out, registry: registry}
}

func (a *App) RunCommand(ctx context.Context, command string) error {
	switch command {
	case "", "providers":
		return a.Providers(ctx)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func (a *App) Run(ctx context.Context, cmd Command) error {
	switch cmd.Name {
	case "", "providers":
		return a.Providers(ctx)
	case "reconfigure":
		return a.Reconfigure(ctx, cmd)
	case "reload":
		return a.Reload(ctx, cmd)
	case "restart":
		return a.Restart(ctx, cmd)
	case "migrate-db":
		return a.MigrateDB(ctx, cmd)
	default:
		return fmt.Errorf("unknown command %q", cmd.Name)
	}
}

func (a *App) Providers(ctx context.Context) error {
	_ = ctx
	for _, provider := range a.registry.Providers() {
		if _, err := fmt.Fprintln(a.out, provider); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Reconfigure(ctx context.Context, cmd Command) error {
	handle, err := a.openMigrated(ctx, cmd)
	if err != nil {
		return err
	}
	defer handle.Close()
	file, err := os.Open(cmd.ConfigPath)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer file.Close()
	cfg, err := appconfig.Decode(file)
	if err != nil {
		return err
	}
	ignore, err := appconfig.LoadIgnoreFile(cmd.IgnorePath)
	if err != nil {
		return fmt.Errorf("load ignore rules: %w", err)
	}
	result, err := appconfig.NewStore(handle).Import(ctx, cfg, ignore, "thelper-ctl")
	if err != nil {
		return err
	}
	return writeJSON(a.out, result)
}

func (a *App) Reload(ctx context.Context, cmd Command) error {
	handle, err := a.openMigrated(ctx, cmd)
	if err != nil {
		return err
	}
	defer handle.Close()
	result, err := appconfig.NewStore(handle).Reload(ctx, nil)
	if err != nil {
		return err
	}
	return writeJSON(a.out, result)
}

func (a *App) Restart(ctx context.Context, cmd Command) error {
	if cmd.ModuleName == "" {
		return fmt.Errorf("restart: module name is required")
	}
	handle, err := a.openMigrated(ctx, cmd)
	if err != nil {
		return err
	}
	defer handle.Close()
	store := modules.NewStore(handle)
	if err := store.Seed(ctx); err != nil {
		return err
	}
	result, err := store.Restart(ctx, cmd.ModuleName, "thelper-ctl")
	if err != nil {
		return err
	}
	return writeJSON(a.out, result)
}

func (a *App) MigrateDB(ctx context.Context, cmd Command) error {
	handle, err := a.openMigrated(ctx, cmd)
	if err != nil {
		return err
	}
	defer handle.Close()
	result, err := appconfig.NewStore(handle).MigrateDB(ctx, a.registry)
	if err != nil {
		return err
	}
	return writeJSON(a.out, result)
}

func (a *App) openMigrated(ctx context.Context, cmd Command) (*storage.Handle, error) {
	provider := cmd.StorageProvider
	if provider == "" {
		provider = "sqlite"
	}
	dsn := cmd.StorageDSN
	if dsn == "" {
		dsn = ".artifacts/dev/sqlite/t-helper.db"
	}
	handle, err := a.registry.Open(ctx, storage.Config{Provider: provider, DSN: dsn})
	if err != nil {
		return nil, err
	}
	if err := a.registry.Migrate(ctx, handle); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
