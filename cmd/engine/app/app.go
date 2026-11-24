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
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
)

const purgeInterval = time.Hour

type Settings struct {
	EventsSubscriptionURL string `env:"EVENTS_SUBSCRIPTION_URL" required:"true"`
	CatalogDatabaseURL    string `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL  string `env:"INCIDENTS_DATABASE_URL" required:"true"`
	EscalationDatabaseURL string `env:"ESCALATION_DATABASE_URL" required:"true"`
}

type App struct {
	Ready        []lifecycle.Check
	subscription messaging.Subscription
	incidents    *incidents.Service
	resources    lifecycle.Resources
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
	a.resources.Add(catalogStorage.Close, catalogStorage.Ping)

	incidentsStorage, err := incidentsdb.Open(ctx, s.IncidentsDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INCIDENTS_DATABASE_URL: %w", err)
	}
	a.resources.Add(incidentsStorage.Close, incidentsStorage.Ping)

	escalationStorage, err := escalationdb.Open(ctx, s.EscalationDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_ESCALATION_DATABASE_URL: %w", err)
	}
	a.resources.Add(escalationStorage.Close, escalationStorage.Ping)

	a.subscription, err = broker.OpenSubscription(ctx, s.EventsSubscriptionURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_SUBSCRIPTION_URL: %w", err)
	}
	a.resources.Add(func() { _ = a.subscription.Close() }, a.subscription.Ping)

	a.incidents = incidents.New(incidentsStorage, catalog.New(catalogStorage, nil), escalation.New(escalationStorage, nil, nil, nil, 0))
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	return lifecycle.Together(ctx,
		messaging.Runner(a.subscription, "engine", handlers.New(a.incidents)),
		lifecycle.Every(purgeInterval, a.incidents.PurgeHandledMessages),
	)
}

func (a *App) Close() {
	a.resources.Close()
}
