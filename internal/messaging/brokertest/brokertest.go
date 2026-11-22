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

type Environment struct {
	Provision func(t *testing.T, topology Topology) string
	Missing   func(t *testing.T) string
}

func Run(t *testing.T, env Environment) {
	open := func(t *testing.T, topology Topology) messaging.Broker {
		t.Helper()
		b, err := broker.Open(t.Context(), env.Provision(t, topology))
		require.NoError(t, err)
		t.Cleanup(func() { _ = b.Close() })
		return b
	}

	t.Run("refuses to open without its infrastructure", func(t *testing.T) {
		b, err := broker.Open(t.Context(), env.Missing(t))
		if err == nil {
			t.Cleanup(func() { _ = b.Close() })
			err = b.Ping(t.Context())
		}
		assert.Error(t, err)
	})

	t.Run("is ready", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})
		assert.NoError(t, b.Ping(t.Context()))
	})

	t.Run("refuses a group whose filter differs from its topics", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}, "notifier": {alert, other}})

		for group, topics := range map[string][]string{"engine": {alert, other}, "notifier": {alert}, "missing": {alert}} {
			_, err := b.Subscribe(t.Context(), group, topics, func(context.Context, messaging.Delivery) error { return nil })
			assert.Error(t, err, group)
		}
	})

	t.Run("delivers a published message", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})
		got := subscribe(t, b, "engine", []string{alert}, accept)

		publish(t, b, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, tick)
		assert.Equal(t, 1, got.all()[0].Attempt)
	})

	t.Run("delivers each message to every group", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}, "notifier": {alert}})
		engine := subscribe(t, b, "engine", []string{alert}, accept)
		notifier := subscribe(t, b, "notifier", []string{alert}, accept)

		publish(t, b, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, engine.data())
			assert.Equal(c, []string{"a"}, notifier.data())
		}, wait, tick)
	})

	t.Run("shares a group's messages between its subscribers", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})
		first := subscribe(t, b, "engine", []string{alert}, accept)
		second := subscribe(t, b, "engine", []string{alert}, accept)

		want := map[string]bool{}
		for i := range 10 {
			want[fmt.Sprint(i)] = true
			publish(t, b, alert, fmt.Sprint(i))
		}

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, want, distinct(append(first.data(), second.data()...)))
		}, wait, tick)
		assert.Less(t, len(first.data())+len(second.data()), 20)
	})

	t.Run("delivers only the topics a group reads", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})
		got := subscribe(t, b, "engine", []string{alert}, accept)

		publish(t, b, other, "ignored")
		publish(t, b, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, tick)
		time.Sleep(time.Second)
		assert.Equal(t, []string{"a"}, got.data())
	})

	t.Run("redelivers a message that failed", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})
		got := subscribe(t, b, "engine", []string{alert}, func(d messaging.Delivery) error {
			if d.Attempt == 1 {
				return errors.New("database unavailable")
			}
			return nil
		})

		publish(t, b, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			deliveries := got.all()
			if assert.Len(c, deliveries, 2) {
				assert.Equal(c, 2, deliveries[1].Attempt)
				assert.Equal(c, "a", string(deliveries[1].Data))
			}
		}, wait, tick)
	})

	t.Run("never redelivers a rejected message", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})
		got := subscribe(t, b, "engine", []string{alert}, func(d messaging.Delivery) error {
			if string(d.Data) == "invalid" {
				return messaging.Reject(errors.New("invalid"))
			}
			return nil
		})

		publish(t, b, alert, "invalid")
		publish(t, b, alert, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.ElementsMatch(c, []string{"invalid", "a"}, got.data())
		}, wait, tick)
		time.Sleep(3 * time.Second)
		assert.ElementsMatch(t, []string{"invalid", "a"}, got.data())
	})

	t.Run("keeps a group's messages while nobody consumes them", func(t *testing.T) {
		b := open(t, Topology{"engine": {alert}})

		publish(t, b, alert, "a")
		got := subscribe(t, b, "engine", []string{alert}, accept)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, tick)
	})
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

func subscribe(t *testing.T, b messaging.Broker, group string, topics []string, handle func(messaging.Delivery) error) *received {
	t.Helper()
	got := &received{}
	subscription, err := b.Subscribe(t.Context(), group, topics, func(_ context.Context, d messaging.Delivery) error {
		got.add(d)
		return handle(d)
	})
	require.NoError(t, err)
	t.Cleanup(subscription.Stop)
	return got
}

func accept(messaging.Delivery) error { return nil }

func publish(t *testing.T, b messaging.Broker, topic string, data string) {
	t.Helper()
	require.NoError(t, b.Publish(t.Context(), topic, uuid.NewV4().String(), []byte(data)))
}
