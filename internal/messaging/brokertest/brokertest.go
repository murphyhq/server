package brokertest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
)

const (
	alert = "murphy.v1.alert"
	other = "murphy.v1.other"
	wait  = 15 * time.Second
	tick  = 50 * time.Millisecond
)

type Topology map[string][]string

type Endpoints struct {
	Topic         string
	Subscriptions map[string]string
}

type Environment struct {
	Provision func(t *testing.T, topology Topology, version int) Endpoints
	Missing   func(t *testing.T) string
}

func Run(t *testing.T, env Environment) {
	provision := func(t *testing.T, topology Topology) (messaging.Topic, func(group string) messaging.Subscription) {
		t.Helper()
		endpoints := env.Provision(t, topology, messaging.TopologyVersion)
		return openTopic(t, endpoints.Topic), func(group string) messaging.Subscription {
			return openSubscription(t, endpoints.Subscriptions[group])
		}
	}

	t.Run("refuses to open without its infrastructure", func(t *testing.T) {
		_, err := broker.OpenTopic(t.Context(), env.Missing(t))
		assert.Error(t, err)
	})

	t.Run("refuses infrastructure older than this release", func(t *testing.T) {
		endpoints := env.Provision(t, Topology{"engine": {alert}}, messaging.TopologyVersion-1)

		_, err := broker.OpenTopic(t.Context(), endpoints.Topic)
		assert.Error(t, err)
		_, err = broker.OpenSubscription(t.Context(), endpoints.Subscriptions["engine"])
		assert.Error(t, err)
	})

	t.Run("is ready", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})
		assert.NoError(t, publisher.Ping(t.Context()))
		assert.NoError(t, consumer("engine").Ping(t.Context()))
	})

	t.Run("delivers a published message", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})
		got := subscribe(t, consumer("engine"), accept)

		publish(t, publisher, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, tick)
		assert.Equal(t, 1, got.all()[0].Attempt)
	})

	t.Run("delivers each message to every group", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}, "notifier": {alert}})
		engine := subscribe(t, consumer("engine"), accept)
		notifier := subscribe(t, consumer("notifier"), accept)

		publish(t, publisher, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, engine.data())
			assert.Equal(c, []string{"a"}, notifier.data())
		}, wait, tick)
	})

	t.Run("shares a group's messages between its subscribers", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})
		first := subscribe(t, consumer("engine"), accept)
		second := subscribe(t, consumer("engine"), accept)

		want := map[string]bool{}
		for i := range 10 {
			want[fmt.Sprint(i)] = true
			publish(t, publisher, alert, fmt.Sprint(i))
		}

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, want, distinct(append(first.data(), second.data()...)))
		}, wait, tick)
		assert.Less(t, len(first.data())+len(second.data()), 20)
	})

	t.Run("delivers only the topics a group reads", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})
		got := subscribe(t, consumer("engine"), accept)

		publish(t, publisher, other, "ignored")
		publish(t, publisher, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, tick)
		time.Sleep(time.Second)
		assert.Equal(t, []string{"a"}, got.data())
	})

	t.Run("redelivers a message that failed", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})
		got := subscribe(t, consumer("engine"), func(d messaging.Delivery) error {
			if d.Attempt == 1 {
				return errors.New("database unavailable")
			}
			return nil
		})

		publish(t, publisher, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			deliveries := got.all()
			if assert.Len(c, deliveries, 2) {
				assert.Equal(c, 2, deliveries[1].Attempt)
				assert.Equal(c, "a", string(deliveries[1].Data))
			}
		}, wait, tick)
	})

	t.Run("never redelivers a rejected message", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})
		got := subscribe(t, consumer("engine"), func(d messaging.Delivery) error {
			if string(d.Data) == "invalid" {
				return messaging.Reject(errors.New("invalid"))
			}
			return nil
		})

		publish(t, publisher, alert, "invalid")
		publish(t, publisher, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.ElementsMatch(c, []string{"invalid", "a"}, got.data())
		}, wait, tick)
		time.Sleep(3 * time.Second)
		assert.ElementsMatch(t, []string{"invalid", "a"}, got.data())
	})

	t.Run("keeps a group's messages while nobody consumes them", func(t *testing.T) {
		publisher, consumer := provision(t, Topology{"engine": {alert}})

		publish(t, publisher, alert, "a")
		got := subscribe(t, consumer("engine"), accept)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, tick)
	})
}

func openTopic(t *testing.T, url string) messaging.Topic {
	t.Helper()
	topic, err := broker.OpenTopic(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = topic.Close() })
	return topic
}

func openSubscription(t *testing.T, url string) messaging.Subscription {
	t.Helper()
	subscription, err := broker.OpenSubscription(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = subscription.Close() })
	return subscription
}

type received struct {
	mu         sync.Mutex
	deliveries []messaging.Delivery
}

func (r *received) add(d messaging.Delivery) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries = append(r.deliveries, d)
}

func (r *received) all() []messaging.Delivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]messaging.Delivery(nil), r.deliveries...)
}

func (r *received) data() []string {
	var data []string
	for _, d := range r.all() {
		data = append(data, string(d.Data))
	}
	return data
}

func distinct(values []string) map[string]bool {
	set := map[string]bool{}
	for _, v := range values {
		set[v] = true
	}
	return set
}

func subscribe(t *testing.T, subscription messaging.Subscription, handle func(messaging.Delivery) error) *received {
	t.Helper()
	got := &received{}
	consumer, err := subscription.Consume(t.Context(), func(_ context.Context, d messaging.Delivery) error {
		got.add(d)
		return handle(d)
	})
	require.NoError(t, err)
	t.Cleanup(consumer.Stop)
	return got
}

func accept(messaging.Delivery) error { return nil }

func publish(t *testing.T, to messaging.Topic, topic string, data string) {
	t.Helper()
	require.NoError(t, to.Publish(t.Context(), topic, uuid.NewV4().String(), []byte(data)))
}
