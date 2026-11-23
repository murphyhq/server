package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/murphyhq/server/cmd/api/router"
	"github.com/murphyhq/server/internal/httpserver"
	idempotencystore "github.com/murphyhq/server/internal/idempotency/store"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
)

type Settings struct {
	HTTPAddr              string   `env:"HTTP_ADDR" default:":8080"`
	CatalogDatabaseURL    string   `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL  string   `env:"INCIDENTS_DATABASE_URL" required:"true"`
	IdempotencyStoreURL   string   `env:"IDEMPOTENCY_STORE_URL" required:"true"`
	IntegrationKeySecrets string   `env:"INTEGRATION_KEY_SECRETS" required:"true"`
	CORSOrigins           []string `env:"CORS_ORIGINS"`
}

type App struct {
	Handler   http.Handler
	Ready     []lifecycle.Check
	resources lifecycle.Resources
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	if err := httpserver.ValidateOrigins(s.CORSOrigins); err != nil {
		return nil, fmt.Errorf("MURPHY_CORS_ORIGINS: %w", err)
	}

	a := &App{}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	catalogStorage, err := catalogdb.Open(ctx, s.CatalogDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_CATALOG_DATABASE_URL: %w", err)
	}
	a.resources.Add(catalogStorage.Close, catalogStorage.Ping)

	incidentsStorage, err := incidentsdb.Open(ctx, s.IncidentsDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INCIDENTS_DATABASE_URL: %w", err)
	}
	a.resources.Add(incidentsStorage.Close, incidentsStorage.Ping)

	keys, err := idempotencystore.Open(ctx, s.IdempotencyStoreURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_IDEMPOTENCY_STORE_URL: %w", err)
	}
	a.resources.Add(func() { _ = keys.Close() }, keys.Ping)

	keyring, err := catalog.ParseKeyring(s.IntegrationKeySecrets)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INTEGRATION_KEY_SECRETS: %w", err)
	}

	catalogService := catalog.New(catalogStorage, keyring)
	handler, err := router.New(catalogService, incidents.New(incidentsStorage, catalogService), keys)
	if err != nil {
		return nil, err
	}
	a.Handler = httpserver.CORS(s.CORSOrigins)(handler)
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Close() {
	a.resources.Close()
}
