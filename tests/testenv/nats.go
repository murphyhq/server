//go:build integration

package testenv

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/internal/nats"
)

func NATS() brokertest.Environment {
	return brokertest.Environment{
		Provision: func(t *testing.T, topology brokertest.Topology) string {
			t.Helper()
			ctx := t.Context()
			stream := name()
			conn, js, err := nats.Open(nats.Config{URL: natsServer()})
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = js.DeleteStream(context.Background(), stream)
				conn.Close()
			})

			_, err = js.CreateStream(ctx, jetstream.StreamConfig{
				Name:      stream,
				Subjects:  []string{stream + ".>"},
				Retention: jetstream.InterestPolicy,
				Storage:   jetstream.MemoryStorage,
			})
			require.NoError(t, err)
			for group, topics := range topology {
				subjects := make([]string, len(topics))
				for i, topic := range topics {
					subjects[i] = stream + "." + topic
				}
				_, err := js.CreateConsumer(ctx, stream, jetstream.ConsumerConfig{
					Durable:        group,
					FilterSubjects: subjects,
					AckPolicy:      jetstream.AckExplicitPolicy,
					AckWait:        30 * time.Second,
					MaxAckPending:  64,
				})
				require.NoError(t, err)
			}
			return natsURL(t, stream)
		},
		Missing: func(t *testing.T) string {
			return natsURL(t, name())
		},
	}
}

func natsServer() string {
	if server := os.Getenv("MURPHY_TEST_NATS_URL"); server != "" {
		return server
	}
	return "nats://localhost:4222"
}

func natsURL(t *testing.T, stream string) string {
	u, err := url.Parse(natsServer())
	require.NoError(t, err)
	u.Path = "/" + stream
	return u.String()
}

func name() string {
	return "test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")[:16]
}
