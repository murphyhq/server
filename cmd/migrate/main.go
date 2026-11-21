package main

import (
	"context"
	"fmt"
	"os"

	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
)

type settings struct {
	Lifecycle          lifecycle.Config
	CatalogDatabaseURL string `env:"CATALOG_DATABASE_URL" required:"true"`
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

	catalogStorage, err := catalogdb.Open(ctx, s.CatalogDatabaseURL)
	if err != nil {
		return fmt.Errorf("MURPHY_CATALOG_DATABASE_URL: %w", err)
	}
	defer catalogStorage.Close()

	return lifecycle.Run(ctx, "migrate", s.Lifecycle, catalogStorage.Migrate)
}
