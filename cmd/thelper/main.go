package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/artBass-rip/t-helper/internal/app/server"
	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	applog "github.com/artBass-rip/t-helper/internal/log"
)

func main() {
	cfg := server.DefaultConfig()
	flag.StringVar(&cfg.ListenAddress, "listen", cfg.ListenAddress, "HTTP listen address")
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "runtime mode")
	flag.StringVar(&cfg.StorageProvider, "storage-provider", cfg.StorageProvider, "storage provider")
	flag.StringVar(&cfg.StorageDSN, "storage-dsn", cfg.StorageDSN, "storage DSN or sqlite path")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	flag.StringVar(&cfg.RuntimeLockPath, "runtime-lock", cfg.RuntimeLockPath, "runtime lock file path")
	flag.BoolVar(&cfg.MigrateOnly, "migrate-only", false, "apply migrations and exit")
	flag.Parse()

	logger := applog.New(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := server.New(cfg, storageproviders.MVPRegistry(), logger)
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		stdlog.Fatal(fmt.Errorf("thelper: %w", err))
	}
}
