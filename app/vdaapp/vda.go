package vdaapp

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// Bus is a long-running component that blocks until its context is cancelled or
// it hits a fatal error.
type Bus interface {
	Start(ctx context.Context) error
}

type App struct {
	buses []Bus
}

func New(buses ...Bus) *App {
	return &App{buses: buses}
}

// Start runs every bus in a fail-fast group: the first fatal error cancels the
// rest and is returned; a cancelled context stops all of them cleanly.
func (a *App) Start(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, b := range a.buses {
		g.Go(func() error {
			return b.Start(gctx)
		})
	}
	return g.Wait()
}
