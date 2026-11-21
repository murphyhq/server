package app

import (
	"context"
	"fmt"

	catalogdb "github.com/murphyhq/server/services/catalog/db"
)

type Settings struct {
	CatalogDatabaseURL string `env:"CATALOG_DATABASE_URL" required:"true"`
}

func Migrate(ctx context.Context, s Settings) error {
	catalogStorage, err := catalogdb.Open(ctx, s.CatalogDatabaseURL)
	if err != nil {
		return fmt.Errorf("MURPHY_CATALOG_DATABASE_URL: %w", err)
	}
	defer catalogStorage.Close()
	return catalogStorage.Migrate(ctx)
}
