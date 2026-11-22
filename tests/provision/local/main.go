package main

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/murphyhq/server/internal/aws"
	murphyconfig "github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/tests/provision"
)

var (
	databases = []string{"catalog", "incidents"}
	topology  = provision.Topology{
		"engine": {"murphy.v1.alert"},
	}
)

type settings struct {
	PostgresURL string `env:"PROVISION_POSTGRES_URL" default:"postgres://murphy:murphy@localhost:5432/murphy?sslmode=disable"`
	NATSURL     string `env:"PROVISION_NATS_URL" default:"nats://localhost:4222"`

	AWSEndpointURL     string `env:"AWS_ENDPOINT_URL" default:"http://localhost:4566"`
	AWSRegion          string `env:"AWS_REGION" default:"us-east-1"`
	AWSAccessKeyID     string `env:"AWS_ACCESS_KEY_ID" default:"test"`
	AWSSecretAccessKey string `env:"AWS_SECRET_ACCESS_KEY" default:"test"`
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
	if err := murphyconfig.Load(&s); err != nil {
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
	cfg, err := aws.Config(ctx,
		config.WithBaseEndpoint(s.AWSEndpointURL),
		config.WithRegion(s.AWSRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.AWSAccessKeyID, s.AWSSecretAccessKey, "")),
	)
	if err != nil {
		return err
	}
	_, err = provision.SNS(ctx, cfg, "murphy", topology, messaging.TopologyVersion)
	return err
}
