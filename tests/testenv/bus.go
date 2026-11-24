//go:build integration

package testenv

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/internal/messaging/brokertest"
)

type Bus struct {
	Publisher *messaging.Publisher
	TopicURL  string
	t         *testing.T
	endpoints brokertest.Endpoints
}

func NewBus(t *testing.T, env brokertest.Environment, topology messaging.Topology) *Bus {
	t.Helper()
	endpoints := env.Provision(t, topology, messaging.TopologyVersion)
	topic, err := broker.OpenTopic(t.Context(), endpoints.Topic)
	require.NoError(t, err)
	t.Cleanup(func() { _ = topic.Close() })
	return &Bus{Publisher: messaging.NewPublisher(topic), TopicURL: endpoints.Topic, t: t, endpoints: endpoints}
}

func (b *Bus) Consume(group string, handlers messaging.Handlers) {
	b.t.Helper()
	subscription, err := broker.OpenSubscription(b.t.Context(), b.endpoints.Subscriptions[group])
	require.NoError(b.t, err)
	b.t.Cleanup(func() { _ = subscription.Close() })
	consumer, err := messaging.Consume(b.t.Context(), subscription, group, handlers)
	require.NoError(b.t, err)
	b.t.Cleanup(consumer.Stop)
}

type Collected struct {
	mu        sync.Mutex
	envelopes []*murphy.Envelope
}

func (b *Bus) Collect(group string, verbs ...protoreflect.Enum) *Collected {
	b.t.Helper()
	collected := &Collected{}
	handlers := make(messaging.Handlers, len(verbs))
	for i, verb := range verbs {
		handlers[i] = messaging.On(verb, func(_ context.Context, envelope *murphy.Envelope) error {
			collected.mu.Lock()
			defer collected.mu.Unlock()
			collected.envelopes = append(collected.envelopes, envelope)
			return nil
		})
	}
	b.Consume(group, handlers)
	return collected
}

func (c *Collected) Envelopes() []*murphy.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.envelopes)
}

func (c *Collected) IDs() []string {
	envelopes := c.Envelopes()
	ids := make([]string, len(envelopes))
	for i, envelope := range envelopes {
		ids[i] = envelope.GetId()
	}
	return ids
}

func RunRelay(t *testing.T, name string, source messaging.ScheduledMessages, publisher *messaging.Publisher) {
	t.Helper()
	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() {
		done <- messaging.Relay(name, source, publisher, messaging.RelaySettings{PollInterval: 50 * time.Millisecond, BatchSize: 10, ClaimLease: 300 * time.Millisecond})(ctx)
	}()
	t.Cleanup(func() {
		stop()
		require.NoError(t, <-done)
	})
}
