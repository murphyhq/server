package main

import (
	"context"
	"os"

	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
)

type settings struct {
	Lifecycle lifecycle.Config
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "api stopped", logger.WithError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	return lifecycle.Run(ctx, "api", s.Lifecycle, lifecycle.UntilStopped)
}
