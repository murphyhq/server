//go:build integration

package testenv

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apiapp "github.com/murphyhq/server/cmd/api/app"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/httpserver"
)

func Databases(t *testing.T, engine DatabaseEngine) migrateapp.Settings {
	t.Helper()
	databases := migrateapp.Settings{
		CatalogDatabaseURL:       engine(t),
		IncidentsDatabaseURL:     engine(t),
		PeopleDatabaseURL:        engine(t),
		OnCallDatabaseURL:        engine(t),
		EscalationDatabaseURL:    engine(t),
		NotificationsDatabaseURL: engine(t),
	}
	require.NoError(t, migrateapp.Migrate(t.Context(), databases))
	return databases
}

func APIWith(t *testing.T, databases migrateapp.Settings) string {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	publicURL := "http://" + server.Listener.Addr().String()
	a, err := apiapp.New(t.Context(), apiapp.Settings{
		CatalogDatabaseURL:    databases.CatalogDatabaseURL,
		IncidentsDatabaseURL:  databases.IncidentsDatabaseURL,
		PeopleDatabaseURL:     databases.PeopleDatabaseURL,
		OnCallDatabaseURL:     databases.OnCallDatabaseURL,
		EscalationDatabaseURL: databases.EscalationDatabaseURL,
		IdempotencyStoreURL:   RedisURL(),
		IntegrationKeySecrets: IntegrationKeySecrets,
		WebhookPublicKeys:     WebhookPublicKey,
		PublicURL:             publicURL,
		OIDCIssuer:            IdentityProviderURL(),
		OIDCClientID:          "murphy",
		OIDCClientSecret:      "murphy-local-secret",
		SessionDuration:       time.Hour,
	})
	require.NoError(t, err)
	t.Cleanup(a.Close)

	server.Config.Handler = httpserver.Handler(a.Handler)
	server.Start()
	t.Cleanup(server.Close)
	return publicURL
}
