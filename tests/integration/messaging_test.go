//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/messaging/broker/nats"
	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/tests/testenv"
)

func TestNATSBroker(t *testing.T) {
	broker, err := nats.Open(t.Context(), testenv.BrokerURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = broker.Close() })
	brokertest.Run(t, broker)
}
