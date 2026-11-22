package main

import (
	"context"
	"os"

	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/tests/provision"
)

var (
	databases = []string{"catalog"}
	topology  = provision.Topology{}
)

type settings struct {
	PostgresURL string `env:"PROVISION_POSTGRES_URL" default:"postgres://murphy:murphy@localhost:5432/murphy?sslmode=disable"`
	NATSURL     string `env:"PROVISION_NATS_URL" default:"nats://localhost:4222"`
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "provisioning failed", telemetry.WithError(err))
		os.Exit(1)
	}
	logger.Info(ctx, "provisioned")
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	for _, database := range databases {
		if err := provision.Database(ctx, s.PostgresURL, database); err != nil {
			return err
		}
	}
	if err := provision.NATS(ctx, s.NATSURL, "events", topology, messaging.TopologyVersion); err != nil {
		return err
	}
	_, err := provision.SNS(ctx, "murphy", topology, messaging.TopologyVersion)
	return err
}
