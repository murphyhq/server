package app

import (
	"context"
	"fmt"
	"time"

	"github.com/murphyhq/server/cmd/engine/handlers"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
)

const purgeInterval = time.Hour

type Settings struct {
	EventsSubscriptionURL string `env:"EVENTS_SUBSCRIPTION_URL" required:"true"`
	CatalogDatabaseURL    string `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL  string `env:"INCIDENTS_DATABASE_URL" required:"true"`
}

type App struct {
	Ready        []lifecycle.Check
	subscription messaging.Subscription
	incidents    *incidents.Service
	closers      lifecycle.Closers
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
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
	a.closers.Add(catalogStorage.Close)

	incidentsStorage, err := incidentsdb.Open(ctx, s.IncidentsDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INCIDENTS_DATABASE_URL: %w", err)
	}
	a.closers.Add(incidentsStorage.Close)

	a.subscription, err = broker.OpenSubscription(ctx, s.EventsSubscriptionURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_SUBSCRIPTION_URL: %w", err)
	}
	a.closers.Add(func() { _ = a.subscription.Close() })

	a.incidents = incidents.New(incidentsStorage, catalog.New(catalogStorage, nil))
	a.Ready = []lifecycle.Check{catalogStorage.Ping, incidentsStorage.Ping, a.subscription.Ping}
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	return lifecycle.Together(ctx,
		messaging.Runner(a.subscription, "engine", handlers.New(a.incidents)),
		lifecycle.Every(purgeInterval, a.incidents.PurgeHandledMessages),
	)
}

func (a *App) Close() {
	a.closers.Close()
}
