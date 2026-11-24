package app

import (
	"context"
	"fmt"

	catalogdb "github.com/murphyhq/server/services/catalog/db"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
	notificationsdb "github.com/murphyhq/server/services/notifications/db"
	oncalldb "github.com/murphyhq/server/services/oncall/db"
	peopledb "github.com/murphyhq/server/services/people/db"
)

type Settings struct {
	CatalogDatabaseURL       string `env:"CATALOG_DATABASE_URL" required:"true"`
	IncidentsDatabaseURL     string `env:"INCIDENTS_DATABASE_URL" required:"true"`
	PeopleDatabaseURL        string `env:"PEOPLE_DATABASE_URL" required:"true"`
	OnCallDatabaseURL        string `env:"ONCALL_DATABASE_URL" required:"true"`
	EscalationDatabaseURL    string `env:"ESCALATION_DATABASE_URL" required:"true"`
	NotificationsDatabaseURL string `env:"NOTIFICATIONS_DATABASE_URL" required:"true"`
}

type storage interface {
	Migrate(ctx context.Context) error
	Close()
}

func Migrate(ctx context.Context, s Settings) error {
	domains := []struct {
		setting string
		open    func() (storage, error)
	}{
		{"MURPHY_CATALOG_DATABASE_URL", func() (storage, error) { return catalogdb.Open(ctx, s.CatalogDatabaseURL) }},
		{"MURPHY_INCIDENTS_DATABASE_URL", func() (storage, error) { return incidentsdb.Open(ctx, s.IncidentsDatabaseURL) }},
		{"MURPHY_PEOPLE_DATABASE_URL", func() (storage, error) { return peopledb.Open(ctx, s.PeopleDatabaseURL) }},
		{"MURPHY_ONCALL_DATABASE_URL", func() (storage, error) { return oncalldb.Open(ctx, s.OnCallDatabaseURL) }},
		{"MURPHY_ESCALATION_DATABASE_URL", func() (storage, error) { return escalationdb.Open(ctx, s.EscalationDatabaseURL) }},
		{"MURPHY_NOTIFICATIONS_DATABASE_URL", func() (storage, error) { return notificationsdb.Open(ctx, s.NotificationsDatabaseURL) }},
	}
	for _, domain := range domains {
		store, err := domain.open()
		if err != nil {
			return fmt.Errorf("%s: %w", domain.setting, err)
		}
		err = store.Migrate(ctx)
		store.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
