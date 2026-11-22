package app

import (
	"context"
	"fmt"

	catalogdb "github.com/murphyhq/server/services/catalog/db"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
)

type Settings struct {
	CatalogDatabaseURL   string `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL string `env:"INCIDENTS_DATABASE_URL" required:"true"`
}

func Migrate(ctx context.Context, s Settings) error {
	catalogStorage, err := catalogdb.Open(ctx, s.CatalogDatabaseURL)
	if err != nil {
		return fmt.Errorf("MURPHY_CATALOG_DATABASE_URL: %w", err)
	}
	defer catalogStorage.Close()
	if err := catalogStorage.Migrate(ctx); err != nil {
		return err
	}

	incidentsStorage, err := incidentsdb.Open(ctx, s.IncidentsDatabaseURL)
	if err != nil {
		return fmt.Errorf("MURPHY_INCIDENTS_DATABASE_URL: %w", err)
	}
	defer incidentsStorage.Close()
	return incidentsStorage.Migrate(ctx)
}
