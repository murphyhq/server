package main

import (
	"context"
	"fmt"
	"os"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
)

type settings struct {
	APIURL  string `env:"SEED_API_URL" default:"http://localhost:8080"`
	Dogfood dogfoodSettings
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "seed failed", telemetry.WithError(err))
		os.Exit(1)
	}
	logger.Info(ctx, "seeded")
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	api, err := client.NewClientWithResponses(s.APIURL)
	if err != nil {
		return fmt.Errorf("create API client: %w", err)
	}
	if err := seedCatalog(ctx, api); err != nil {
		return err
	}
	return seedDogfood(ctx, api, s.Dogfood)
}
