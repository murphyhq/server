//go:build integration

package testenv

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/nats"
)

func BrokerURL(t *testing.T) string {
	t.Helper()
	server := os.Getenv("MURPHY_TEST_NATS_URL")
	if server == "" {
		server = "nats://localhost:4222"
	}
	stream := "test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	t.Cleanup(func() {
		conn, js, err := nats.Open(nats.Config{URL: server})
		if err != nil {
			return
		}
		defer conn.Close()
		_ = js.DeleteStream(context.Background(), stream)
	})

	u, err := url.Parse(server)
	require.NoError(t, err)
	u.Path = "/" + stream
	return u.String()
}
