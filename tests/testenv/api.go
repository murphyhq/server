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

func API(t *testing.T) (*client.ClientWithResponses, string) {
	t.Helper()
	ctx := t.Context()

	databases := migrateapp.Settings{CatalogDatabaseURL: Database(t)}
	require.NoError(t, migrateapp.Migrate(ctx, databases))

	a, err := apiapp.New(ctx, apiapp.Settings{
		CatalogDatabaseURL:  databases.CatalogDatabaseURL,
		IdempotencyStoreURL: RedisURL(),
	})
	require.NoError(t, err)
	t.Cleanup(a.Close)

	server := httptest.NewServer(httpserver.Handler(a.Handler))
	t.Cleanup(server.Close)

	api, err := client.NewClientWithResponses(server.URL)
	require.NoError(t, err)
	return api, server.URL
}
