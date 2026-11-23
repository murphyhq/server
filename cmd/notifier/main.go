package main

import (
	"context"
	"os"

	"github.com/murphyhq/server/cmd/notifier/app"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
)

type settings struct {
	Lifecycle lifecycle.Config
	App       app.Settings
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "notifier stopped", telemetry.WithError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}

	a, err := app.New(ctx, s.App)
	if err != nil {
		return err
	}
	defer a.Close()

	return lifecycle.Run(ctx, "notifier", s.Lifecycle, a.Run, a.Ready...)
}
