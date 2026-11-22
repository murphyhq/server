package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/murphyhq/server/cmd/ingest/router"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/catalog"
)

type Settings struct {
	HTTPAddr              string `env:"HTTP_ADDR" default:":8080"`
	EventsTopicURL        string `env:"EVENTS_TOPIC_URL" required:"true"`
	IntegrationKeySecrets string `env:"INTEGRATION_KEY_SECRETS" required:"true"`
}

type App struct {
	Handler http.Handler
	Ready   []lifecycle.Check
	closers lifecycle.Closers
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	a := &App{}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	keyring, err := catalog.ParseKeyring(s.IntegrationKeySecrets)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INTEGRATION_KEY_SECRETS: %w", err)
	}

	topic, err := broker.OpenTopic(ctx, s.EventsTopicURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_TOPIC_URL: %w", err)
	}
	a.closers.Add(func() { _ = topic.Close() })

	a.Handler, err = router.New(messaging.NewPublisher(topic), keyring)
	if err != nil {
		return nil, err
	}
	a.Ready = []lifecycle.Check{topic.Ping}
	return a, nil
}

func (a *App) Close() {
	a.closers.Close()
}
