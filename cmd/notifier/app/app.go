package app

import (
	"context"
	"fmt"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/cmd/notifier/handlers"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	"github.com/murphyhq/server/services/notifications"
	notificationsdb "github.com/murphyhq/server/services/notifications/db"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
	"github.com/murphyhq/server/services/oncall"
	oncalldb "github.com/murphyhq/server/services/oncall/db"
	"github.com/murphyhq/server/services/people"
	peopledb "github.com/murphyhq/server/services/people/db"
)

const purgeInterval = time.Hour

type Settings struct {
	EventsSubscriptionURL    string `env:"EVENTS_SUBSCRIPTION_URL" required:"true"`
	EventsTopicURL           string `env:"EVENTS_TOPIC_URL" required:"true"`
	NotificationsDatabaseURL string `env:"NOTIFICATIONS_DATABASE_URL" required:"true"`
	PeopleDatabaseURL        string `env:"PEOPLE_DATABASE_URL" required:"true"`
	OnCallDatabaseURL        string `env:"ONCALL_DATABASE_URL" required:"true"`
	EscalationDatabaseURL    string `env:"ESCALATION_DATABASE_URL" required:"true"`
	WebhookSigningKeys       string `env:"WEBHOOK_SIGNING_KEYS" required:"true"`
}

type App struct {
	Ready         []lifecycle.Check
	subscription  messaging.Subscription
	notifications *notifications.Service
	sender        *notifications.Sender
	resources     lifecycle.Resources
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	a := &App{}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	keys, err := webhook.ParseSigningKeys(s.WebhookSigningKeys)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_WEBHOOK_SIGNING_KEYS: %w", err)
	}

	notificationsStorage, err := notificationsdb.Open(ctx, s.NotificationsDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_NOTIFICATIONS_DATABASE_URL: %w", err)
	}
	a.resources.Add(notificationsStorage.Close, notificationsStorage.Ping)

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

	topic, err := broker.OpenTopic(ctx, s.EventsTopicURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_TOPIC_URL: %w", err)
	}
	a.resources.Add(func() { _ = topic.Close() }, topic.Ping)

	a.subscription, err = broker.OpenSubscription(ctx, s.EventsSubscriptionURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_SUBSCRIPTION_URL: %w", err)
	}
	a.resources.Add(func() { _ = a.subscription.Close() }, a.subscription.Ping)

	peopleService := people.New(peopleStorage)
	a.notifications = notifications.New(notificationsStorage, escalation.New(escalationStorage, nil, nil, nil, 0), oncall.New(oncallStorage, peopleService), peopleService)
	a.sender = notifications.NewSender(messaging.NewPublisher(topic), map[murphy.ContactMethodType]notifications.Provider{
		murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK: webhook.New(keys),
	})
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	return lifecycle.Together(ctx,
		messaging.Runner(a.subscription, "notifier", handlers.New(a.notifications, a.sender)),
		lifecycle.Every(purgeInterval, a.notifications.PurgeHandledMessages),
	)
}

func (a *App) Close() {
	a.resources.Close()
}
