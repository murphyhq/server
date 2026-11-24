package main

import (
	"context"
	"os"

	apiapp "github.com/murphyhq/server/cmd/api/app"
	engineapp "github.com/murphyhq/server/cmd/engine/app"
	ingestapp "github.com/murphyhq/server/cmd/ingest/app"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	notifierapp "github.com/murphyhq/server/cmd/notifier/app"
	schedulerapp "github.com/murphyhq/server/cmd/scheduler/app"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
)

type settings struct {
	Lifecycle lifecycle.Config
	Migrate   migrateapp.Settings
	API       apiapp.Settings
	Ingest    ingestapp.Settings
	Engine    engineapp.Settings
	Scheduler schedulerapp.Settings
	Notifier  notifierapp.Settings
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "murphy stopped", telemetry.WithError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	if err := migrateapp.Migrate(ctx, s.Migrate); err != nil {
		return err
	}

	api, err := apiapp.New(telemetry.WithComponent(ctx, "api"), s.API)
	if err != nil {
		return err
	}
	defer api.Close()
	ingest, err := ingestapp.New(telemetry.WithComponent(ctx, "ingest"), s.Ingest)
	if err != nil {
		return err
	}
	defer ingest.Close()
	engine, err := engineapp.New(telemetry.WithComponent(ctx, "engine"), s.Engine)
	if err != nil {
		return err
	}
	defer engine.Close()
	scheduler, err := schedulerapp.New(telemetry.WithComponent(ctx, "scheduler"), s.Scheduler)
	if err != nil {
		return err
	}
	defer scheduler.Close()
	notifier, err := notifierapp.New(telemetry.WithComponent(ctx, "notifier"), s.Notifier)
	if err != nil {
		return err
	}
	defer notifier.Close()

	ready := append(append(append(append(api.Ready, ingest.Ready...), engine.Ready...), scheduler.Ready...), notifier.Ready...)
	timeout := s.Lifecycle.ShutdownTimeout
	return lifecycle.Run(ctx, "murphy", s.Lifecycle, func(ctx context.Context) error {
		return lifecycle.Together(ctx,
			lifecycle.Component("api", func(ctx context.Context) error { return api.Serve(ctx, timeout) }),
			lifecycle.Component("ingest", func(ctx context.Context) error { return ingest.Serve(ctx, timeout) }),
			lifecycle.Component("engine", engine.Run),
			lifecycle.Component("scheduler", scheduler.Run),
			lifecycle.Component("notifier", notifier.Run),
		)
	}, ready...)
}
