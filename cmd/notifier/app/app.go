package app

import (
	"context"
	"fmt"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/cmd/notifier/handlers"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/notifications"
	"github.com/murphyhq/server/services/notifications/webhook"
)

type Settings struct {
	EventsSubscriptionURL string `env:"EVENTS_SUBSCRIPTION_URL" required:"true"`
	EventsTopicURL        string `env:"EVENTS_TOPIC_URL" required:"true"`
	WebhookSigningKeys    string `env:"WEBHOOK_SIGNING_KEYS" required:"true"`
}

type App struct {
	Ready         []lifecycle.Check
	subscription  messaging.Subscription
	notifications *notifications.Service
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

	a.notifications = notifications.New(messaging.NewPublisher(topic), map[murphy.ContactMethodType]notifications.Provider{
		murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK: webhook.New(keys),
	})
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	return messaging.Runner(a.subscription, "notifier", handlers.New(a.notifications))(ctx)
}

func (a *App) Close() {
	a.resources.Close()
}
