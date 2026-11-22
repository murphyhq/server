//go:build integration

package testenv

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	ingestapp "github.com/murphyhq/server/cmd/ingest/app"
	"github.com/murphyhq/server/internal/httpserver"
)

func Ingest(t *testing.T, topicURL string) string {
	t.Helper()
	a, err := ingestapp.New(t.Context(), ingestapp.Settings{
		EventsTopicURL:        topicURL,
		IntegrationKeySecrets: IntegrationKeySecrets,
	})
	require.NoError(t, err)
	t.Cleanup(a.Close)

	server := httptest.NewServer(httpserver.Handler(a.Handler))
	t.Cleanup(server.Close)
	return server.URL
}
