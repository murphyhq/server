package main

import (
	"context"
	"os"

	"github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
)

type settings struct {
	Lifecycle lifecycle.Config
	App       app.Settings
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "migrate stopped", logger.WithError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	return lifecycle.Run(ctx, "migrate", s.Lifecycle, func(ctx context.Context) error {
		return app.Migrate(ctx, s.App)
	})
}
