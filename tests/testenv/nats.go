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

	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/tests/provision"
)

func NATS() brokertest.Environment {
	return brokertest.Environment{
		Provision: func(t *testing.T, topology brokertest.Topology, version int) brokertest.Endpoints {
			t.Helper()
			stream := name()
			t.Cleanup(func() { _ = provision.DropNATS(context.Background(), natsServer(), stream) })
			require.NoError(t, provision.NATS(t.Context(), natsServer(), stream, provision.Topology(topology), version))

			endpoints := brokertest.Endpoints{Publisher: natsURL(t, stream, ""), Consumers: map[string]string{}}
			for group := range topology {
				endpoints.Consumers[group] = natsURL(t, stream, group)
			}
			return endpoints
		},
		Missing: func(t *testing.T) string {
			return natsURL(t, name(), "")
		},
	}
}

func natsServer() string {
	if server := os.Getenv("MURPHY_TEST_NATS_URL"); server != "" {
		return server
	}
	return "nats://localhost:4222"
}

func natsURL(t *testing.T, stream, consumer string) string {
	u, err := url.Parse(natsServer())
	require.NoError(t, err)
	u.Path = "/" + stream
	if consumer != "" {
		u.RawQuery = url.Values{"consumer": {consumer}}.Encode()
	}
	return u.String()
}

func name() string {
	return "test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")[:16]
}
