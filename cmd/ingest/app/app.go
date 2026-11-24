package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/murphyhq/server/cmd/ingest/router"
	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/catalog"
)

type Settings struct {
	Addr                  string   `env:"INGEST_ADDR" default:":8081"`
	EventsTopicURL        string   `env:"EVENTS_TOPIC_URL" required:"true"`
	IntegrationKeySecrets string   `env:"INTEGRATION_KEY_SECRETS" required:"true"`
	CORSOrigins           []string `env:"CORS_ORIGINS" validate:"base-url"`
}

type App struct {
	Handler   http.Handler
	Ready     []lifecycle.Check
	resources lifecycle.Resources
	addr      string
	cors      httpserver.CORS
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	a := &App{addr: s.Addr, cors: s.CORS()}
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
	a.resources.Add(func() { _ = topic.Close() }, topic.Ping)

	handler, err := router.New(messaging.NewPublisher(topic), keyring)
	if err != nil {
		return nil, err
	}
	a.Handler = handler
	a.Ready = a.resources.Checks()
	return a, nil
}

func (a *App) Close() {
	a.resources.Close()
}

func (s Settings) CORS() httpserver.CORS {
	return httpserver.CORS{
		Origins:       s.CORSOrigins,
		Methods:       []string{"POST"},
		AllowHeaders:  []string{"Authorization", "Content-Type"},
		ExposeHeaders: []string{"WWW-Authenticate"},
	}
}

func (a *App) Serve(ctx context.Context, shutdownTimeout time.Duration) error {
	return httpserver.New(httpserver.Config{Addr: a.addr, ShutdownTimeout: shutdownTimeout, CORS: a.cors}, a.Handler).Serve(ctx)
}
