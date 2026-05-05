package ctl

import (
	"context"
	"fmt"
	"io"

	"github.com/artBass-rip/t-helper/internal/storage"
)

type App struct {
	out      io.Writer
	registry *storage.Registry
}

func New(out io.Writer, registry *storage.Registry) *App {
	return &App{out: out, registry: registry}
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
