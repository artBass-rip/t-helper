package worker

import (
	"context"
	"log/slog"
)

type App struct {
	logger *slog.Logger
}

func New(logger *slog.Logger) *App {
	return &App{logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	a.logger.Info("thelper-worker scaffold ready; job execution starts in Stage 03")
	<-ctx.Done()
	return ctx.Err()
}
