package main

import (
	"context"
	"fmt"
	"os"

	"github.com/murphyhq/server/cmd/api/router"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/httpserver"
	idempotencystore "github.com/murphyhq/server/internal/idempotency/store"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
)

type settings struct {
	Lifecycle           lifecycle.Config
	HTTPAddr            string `env:"HTTP_ADDR" default:":8080"`
	CatalogDatabaseURL  string `env:"CATALOG_DATABASE_URL" required:"true"`
	IdempotencyStoreURL string `env:"IDEMPOTENCY_STORE_URL" required:"true"`
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

	catalogStorage, err := catalogdb.Open(ctx, s.CatalogDatabaseURL)
	if err != nil {
		return fmt.Errorf("MURPHY_CATALOG_DATABASE_URL: %w", err)
	}
	defer catalogStorage.Close()

	keys, err := idempotencystore.Open(ctx, s.IdempotencyStoreURL)
	if err != nil {
		return fmt.Errorf("MURPHY_IDEMPOTENCY_STORE_URL: %w", err)
	}
	defer func() { _ = keys.Close() }()

	handler, err := router.New(catalog.New(catalogStorage), keys)
	if err != nil {
		return err
	}

	return lifecycle.Run(ctx, "api", s.Lifecycle, func(ctx context.Context) error {
		return httpserver.Serve(ctx, httpserver.Config{Addr: s.HTTPAddr, ShutdownTimeout: s.Lifecycle.ShutdownTimeout}, handler)
	}, catalogStorage.Ping, keys.Ping)
}
