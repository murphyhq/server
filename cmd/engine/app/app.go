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
	"github.com/murphyhq/server/services/oncall"
	oncalldb "github.com/murphyhq/server/services/oncall/db"
	"github.com/murphyhq/server/services/people"
	peopledb "github.com/murphyhq/server/services/people/db"
)

const purgeInterval = time.Hour

type Settings struct {
	EventsSubscriptionURL string `env:"EVENTS_SUBSCRIPTION_URL" required:"true"`
	CatalogDatabaseURL    string `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL  string `env:"INCIDENTS_DATABASE_URL" required:"true"`
	PeopleDatabaseURL     string `env:"PEOPLE_DATABASE_URL" required:"true"`
	OnCallDatabaseURL     string `env:"ONCALL_DATABASE_URL" required:"true"`
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

	peopleStorage, err := peopledb.Open(ctx, s.PeopleDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_PEOPLE_DATABASE_URL: %w", err)
	}
	a.resources.Add(peopleStorage.Close, peopleStorage.Ping)

	oncallStorage, err := oncalldb.Open(ctx, s.OnCallDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_ONCALL_DATABASE_URL: %w", err)
	}
	a.resources.Add(oncallStorage.Close, oncallStorage.Ping)

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

	catalogService := catalog.New(catalogStorage, nil)
	peopleService := people.New(peopleStorage)
	oncallService := oncall.New(oncallStorage, peopleService)
	escalationService := escalation.New(escalationStorage, peopleService, oncallService, catalogService)
	a.incidents = incidents.New(incidentsStorage, catalogService, escalationService, oncallService, peopleService)
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
