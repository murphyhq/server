package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/murphyhq/server/cmd/api/router"
	"github.com/murphyhq/server/internal/httpserver"
	idempotencystore "github.com/murphyhq/server/internal/idempotency/store"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
	"github.com/murphyhq/server/services/oncall"
	oncalldb "github.com/murphyhq/server/services/oncall/db"
	"github.com/murphyhq/server/services/people"
	peopledb "github.com/murphyhq/server/services/people/db"
)

type Settings struct {
	HTTPAddr               string        `env:"HTTP_ADDR" default:":8080"`
	CatalogDatabaseURL     string        `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL   string        `env:"INCIDENTS_DATABASE_URL" required:"true"`
	PeopleDatabaseURL      string        `env:"PEOPLE_DATABASE_URL" required:"true"`
	OnCallDatabaseURL      string        `env:"ONCALL_DATABASE_URL" required:"true"`
	EscalationDatabaseURL  string        `env:"ESCALATION_DATABASE_URL" required:"true"`
	IdempotencyStoreURL    string        `env:"IDEMPOTENCY_STORE_URL" required:"true"`
	IntegrationKeySecrets  string        `env:"INTEGRATION_KEY_SECRETS" required:"true"`
	WebhookPublicKeys      string        `env:"WEBHOOK_PUBLIC_KEYS" required:"true"`
	EscalationMinimumDelay time.Duration `env:"ESCALATION_MINIMUM_DELAY" default:"1m"`
	CORSOrigins            []string      `env:"CORS_ORIGINS" validate:"base-url"`
}

type App struct {
	Handler   http.Handler
	Ready     []lifecycle.Check
	resources lifecycle.Resources
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

	keys, err := idempotencystore.Open(ctx, s.IdempotencyStoreURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_IDEMPOTENCY_STORE_URL: %w", err)
	}
	a.resources.Add(func() { _ = keys.Close() }, keys.Ping)

	keyring, err := catalog.ParseKeyring(s.IntegrationKeySecrets)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INTEGRATION_KEY_SECRETS: %w", err)
	}

	publicKeys, err := webhook.ParsePublicKeys(s.WebhookPublicKeys)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_WEBHOOK_PUBLIC_KEYS: %w", err)
	}

	catalogService := catalog.New(catalogStorage, keyring)
	peopleService := people.New(peopleStorage)
	oncallService := oncall.New(oncallStorage, peopleService)
	escalationService := escalation.New(escalationStorage, peopleService, oncallService, catalogService, s.EscalationMinimumDelay)
	a.Handler, err = router.New(router.Services{
		Catalog:    catalogService,
		Incidents:  incidents.New(incidentsStorage, catalogService, escalationService),
		People:     peopleService,
		OnCall:     oncallService,
		Escalation: escalationService,
		PublicKeys: publicKeys,
	}, keys)
	if err != nil {
		return nil, err
	}
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Close() {
	a.resources.Close()
}

func (s Settings) CORS() httpserver.CORS {
	return httpserver.CORS{
		Origins:       s.CORSOrigins,
		Methods:       []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"},
		AllowHeaders:  []string{"Authorization", "Content-Type", "Idempotency-Key"},
		ExposeHeaders: []string{"Idempotent-Replayed"},
	}
}
