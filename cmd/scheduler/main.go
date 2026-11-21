package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/lifecycle"
)

type settings struct {
	Lifecycle lifecycle.Config
}

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("scheduler stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(ctx, &s); err != nil {
		return err
	}
	return lifecycle.Run(ctx, s.Lifecycle, lifecycle.UntilStopped)
}
