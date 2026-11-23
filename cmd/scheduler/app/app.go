package app

import (
	"context"
	"fmt"

	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
)

type Settings struct {
	EventsTopicURL       string `env:"EVENTS_TOPIC_URL" required:"true"`
	IncidentsDatabaseURL string `env:"INCIDENTS_DATABASE_URL" required:"true"`
	Relay                messaging.RelaySettings
}

type App struct {
	Ready     []lifecycle.Check
	relays    []func(context.Context) error
	resources lifecycle.Resources
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	a := &App{}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	storage, err := incidentsdb.Open(ctx, s.IncidentsDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_INCIDENTS_DATABASE_URL: %w", err)
	}
	a.resources.Add(storage.Close, storage.Ping)

	topic, err := broker.OpenTopic(ctx, s.EventsTopicURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_EVENTS_TOPIC_URL: %w", err)
	}
	a.resources.Add(func() { _ = topic.Close() }, topic.Ping)

	publisher := messaging.NewPublisher(topic)
	a.relays = []func(context.Context) error{
		messaging.Relay("incidents", incidents.New(storage, nil, nil, nil, nil), publisher, s.Relay),
	}
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	return lifecycle.Together(ctx, a.relays...)
}

func (a *App) Close() {
	a.resources.Close()
}
