package app

import (
	"context"
	"fmt"
	"time"

	"github.com/murphyhq/server/cmd/engine/handlers"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/internal/telemetry"
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
	closers      []func()
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
	a.closers = append(a.closers, catalogStorage.Close)

	incidentsStorage, err := incidentsdb.Open(ctx, s.IncidentsDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INCIDENTS_DATABASE_URL: %w", err)
	}
	a.closers = append(a.closers, incidentsStorage.Close)

	a.subscription, err = broker.OpenSubscription(ctx, s.EventsSubscriptionURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_SUBSCRIPTION_URL: %w", err)
	}
	a.closers = append(a.closers, func() { _ = a.subscription.Close() })

	a.incidents = incidents.New(incidentsStorage, catalog.New(catalogStorage, nil))
	a.Ready = []lifecycle.Check{catalogStorage.Ping, incidentsStorage.Ping, a.subscription.Ping}
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	consumer, err := messaging.Consume(ctx, a.subscription, "engine", handlers.New(a.incidents))
	if err != nil {
		return err
	}
	defer consumer.Stop()

	ticker := time.NewTicker(purgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.incidents.PurgeHandledMessages(ctx); err != nil {
				logger.Warn(ctx, "purging handled messages failed", telemetry.WithError(err))
			}
		}
	}
}

func (a *App) Close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
}
