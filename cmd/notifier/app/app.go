package app

import (
	"context"
	"fmt"

	"github.com/murphyhq/server/cmd/notifier/handlers"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
)

type Settings struct {
	EventsSubscriptionURL string `env:"EVENTS_SUBSCRIPTION_URL" required:"true"`
}

type App struct {
	Ready        []lifecycle.Check
	subscription messaging.Subscription
	resources    lifecycle.Resources
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	a := &App{}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	a.subscription, err = broker.OpenSubscription(ctx, s.EventsSubscriptionURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_SUBSCRIPTION_URL: %w", err)
	}
	a.resources.Add(func() { _ = a.subscription.Close() }, a.subscription.Ping)
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	return messaging.Runner(a.subscription, "notifier", handlers.New())(ctx)
}

func (a *App) Close() {
	a.resources.Close()
}
