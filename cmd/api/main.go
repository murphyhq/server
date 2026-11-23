package main

import (
	"context"
	"fmt"
	"os"

	"github.com/murphyhq/server/cmd/api/app"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/httpserver"
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
		logger.Error(ctx, "api stopped", telemetry.WithError(err))
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

	server, err := httpserver.New(httpserver.Config{
		Addr:            s.App.HTTPAddr,
		ShutdownTimeout: s.Lifecycle.ShutdownTimeout,
		CORS:            s.App.CORS(),
	}, a.Handler)
	if err != nil {
		return fmt.Errorf("MURPHY_CORS_ORIGINS: %w", err)
	}

	return lifecycle.Run(ctx, "api", s.Lifecycle, func(ctx context.Context) error {
		return server.Serve(ctx)
	}, a.Ready...)
}
