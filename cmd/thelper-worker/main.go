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

	"github.com/artBass-rip/t-helper/internal/app/worker"
	applog "github.com/artBass-rip/t-helper/internal/log"
)

func main() {
	logLevel := flag.String("log-level", "info", "log level")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := worker.New(applog.New(*logLevel))
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		stdlog.Fatal(fmt.Errorf("thelper-worker: %w", err))
	}
}
