//go:build integration

package testenv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	engineapp "github.com/murphyhq/server/cmd/engine/app"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/brokertest"
)

type Stack struct {
	API       *client.ClientWithResponses
	IngestURL string
	Databases migrateapp.Settings
}

func Murphy(t *testing.T, broker brokertest.Environment) Stack {
	t.Helper()
	databases := Databases(t)
	endpoints := broker.Provision(t, brokertest.Topology{"engine": {"murphy.v1.alert"}}, messaging.TopologyVersion)

	api, _ := APIWith(t, databases)
	ingest := Ingest(t, endpoints.Topic)
	Engine(t, engineapp.Settings{
		EventsSubscriptionURL: endpoints.Subscriptions["engine"],
		CatalogDatabaseURL:    databases.CatalogDatabaseURL,
		IncidentsDatabaseURL:  databases.IncidentsDatabaseURL,
	})
	return Stack{API: api, IngestURL: ingest, Databases: databases}
}

func Engine(t *testing.T, settings engineapp.Settings) {
	t.Helper()
	a, err := engineapp.New(t.Context(), settings)
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
