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

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
	"github.com/artBass-rip/t-helper/internal/app/worker"
	applog "github.com/artBass-rip/t-helper/internal/log"
)

func main() {
	cfg := worker.DefaultConfig()
	flag.StringVar(&cfg.StorageProvider, "storage-provider", cfg.StorageProvider, "storage provider")
	flag.StringVar(&cfg.StorageDSN, "storage-dsn", cfg.StorageDSN, "storage DSN or sqlite path")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	flag.DurationVar(&cfg.PollInterval, "poll-interval", cfg.PollInterval, "worker poll interval")
	flag.DurationVar(&cfg.LeaseDuration, "lease-duration", cfg.LeaseDuration, "job lease duration")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "job heartbeat interval")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := worker.New(cfg, storageproviders.MVPRegistry(), applog.New(cfg.LogLevel))
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		stdlog.Fatal(fmt.Errorf("thelper-worker: %w", err))
	}
}
