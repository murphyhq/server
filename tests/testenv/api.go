//go:build integration

package testenv

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	apiapp "github.com/murphyhq/server/cmd/api/app"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/httpserver"
)

func Databases(t *testing.T, engine DatabaseEngine) migrateapp.Settings {
	t.Helper()
	databases := migrateapp.Settings{
		CatalogDatabaseURL:    engine(t),
		IncidentsDatabaseURL:  engine(t),
		PeopleDatabaseURL:     engine(t),
		OnCallDatabaseURL:     engine(t),
		EscalationDatabaseURL: engine(t),
	}
	require.NoError(t, migrateapp.Migrate(t.Context(), databases))
	return databases
}

func API(t *testing.T, engine DatabaseEngine) (*client.ClientWithResponses, string) {
	t.Helper()
	return APIWith(t, Databases(t, engine))
}

func APIWith(t *testing.T, databases migrateapp.Settings) (*client.ClientWithResponses, string) {
	t.Helper()
	a, err := apiapp.New(t.Context(), apiapp.Settings{
		CatalogDatabaseURL:    databases.CatalogDatabaseURL,
		IncidentsDatabaseURL:  databases.IncidentsDatabaseURL,
		PeopleDatabaseURL:     databases.PeopleDatabaseURL,
		OnCallDatabaseURL:     databases.OnCallDatabaseURL,
		EscalationDatabaseURL: databases.EscalationDatabaseURL,
		IdempotencyStoreURL:   RedisURL(),
		IntegrationKeySecrets: IntegrationKeySecrets,
		WebhookPublicKeys:     WebhookPublicKey,
	})
	require.NoError(t, err)
	t.Cleanup(a.Close)

	server := httptest.NewServer(httpserver.Handler(a.Handler))
	t.Cleanup(server.Close)

	api, err := client.NewClientWithResponses(server.URL)
	require.NoError(t, err)
	return api, server.URL
}
