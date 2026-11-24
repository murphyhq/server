//go:build integration

package testenv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	engineapp "github.com/murphyhq/server/cmd/engine/app"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	notifierapp "github.com/murphyhq/server/cmd/notifier/app"
	schedulerapp "github.com/murphyhq/server/cmd/scheduler/app"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/brokertest"
)

type Stack struct {
	API       *client.ClientWithResponses
	IngestURL string
	Databases migrateapp.Settings
}

func Murphy(t *testing.T, broker brokertest.Environment, engine DatabaseEngine) Stack {
	t.Helper()
	databases := Databases(t, engine)
	endpoints := broker.Provision(t, messaging.Topology{
		"engine":   {"murphy.v1.alert": {"received"}, "murphy.v1.timer": {"expired"}, "murphy.v1.notification": {"sent", "failed"}},
		"notifier": {"murphy.v1.notification": {"created"}},
	}, messaging.TopologyVersion)

	api, _ := APIWith(t, databases)
	ingest := Ingest(t, endpoints.Topic)
	Engine(t, engineapp.Settings{
		EventsSubscriptionURL: endpoints.Subscriptions["engine"],
		CatalogDatabaseURL:    databases.CatalogDatabaseURL,
		IncidentsDatabaseURL:  databases.IncidentsDatabaseURL,
		PeopleDatabaseURL:     databases.PeopleDatabaseURL,
		OnCallDatabaseURL:     databases.OnCallDatabaseURL,
		EscalationDatabaseURL: databases.EscalationDatabaseURL,
	})
	run(t, func(ctx context.Context) (runner, error) {
		return schedulerapp.New(ctx, schedulerapp.Settings{
			EventsTopicURL:       endpoints.Topic,
			IncidentsDatabaseURL: databases.IncidentsDatabaseURL,
			Relay:                messaging.RelaySettings{PollInterval: 50 * time.Millisecond, BatchSize: 100, ClaimLease: 10 * time.Second},
		})
	})
	run(t, func(ctx context.Context) (runner, error) {
		return notifierapp.New(ctx, notifierapp.Settings{EventsSubscriptionURL: endpoints.Subscriptions["notifier"]})
	})
	return Stack{API: api, IngestURL: ingest, Databases: databases}
}

type runner interface {
	Run(ctx context.Context) error
	Close()
}

func Engine(t *testing.T, settings engineapp.Settings) {
	t.Helper()
	run(t, func(ctx context.Context) (runner, error) { return engineapp.New(ctx, settings) })
}

func run(t *testing.T, open func(ctx context.Context) (runner, error)) {
	t.Helper()
	a, err := open(t.Context())
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		stop()
		require.NoError(t, <-done)
		a.Close()
	})
}
