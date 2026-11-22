package brokertest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/messaging"
)

const wait = 10 * time.Second

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

func subscribe(t *testing.T, broker messaging.Broker, group string, topics []string, handle func(messaging.Delivery) error) *received {
	t.Helper()
	got := &received{}
	subscription, err := broker.Subscribe(t.Context(), group, topics, func(_ context.Context, d messaging.Delivery) error {
		got.add(d)
		return handle(d)
	})
	require.NoError(t, err)
	t.Cleanup(subscription.Stop)
	return got
}

func accept(messaging.Delivery) error { return nil }

func publish(t *testing.T, broker messaging.Broker, topic string, data string) {
	t.Helper()
	require.NoError(t, broker.Publish(t.Context(), topic, "key", uuid.NewV4().String(), []byte(data)))
}

func group() string {
	return "group_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
}

func Run(t *testing.T, broker messaging.Broker) {
	const topic, other = "murphy.v1.alert", "murphy.v1.other"

	t.Run("is ready", func(t *testing.T) {
		assert.NoError(t, broker.Ping(t.Context()))
	})

	t.Run("delivers a published message", func(t *testing.T) {
		got := subscribe(t, broker, group(), []string{topic}, accept)

		publish(t, broker, topic, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, 50*time.Millisecond)
		assert.Equal(t, 1, got.all()[0].Attempt)
	})

	t.Run("delivers each message to every group", func(t *testing.T) {
		first := subscribe(t, broker, group(), []string{topic}, accept)
		second := subscribe(t, broker, group(), []string{topic}, accept)

		publish(t, broker, topic, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, first.data())
			assert.Equal(c, []string{"a"}, second.data())
		}, wait, 50*time.Millisecond)
	})

	t.Run("delivers each message once within a group", func(t *testing.T) {
		name := group()
		first := subscribe(t, broker, name, []string{topic}, accept)
		second := subscribe(t, broker, name, []string{topic}, accept)

		for i := range 10 {
			publish(t, broker, topic, fmt.Sprint(i))
		}

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Len(c, append(first.data(), second.data()...), 10)
		}, wait, 50*time.Millisecond)
		time.Sleep(time.Second)
		assert.ElementsMatch(t, []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}, append(first.data(), second.data()...))
	})

	t.Run("delivers only the topics subscribed to", func(t *testing.T) {
		got := subscribe(t, broker, group(), []string{topic}, accept)

		publish(t, broker, other, "ignored")
		publish(t, broker, topic, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, 50*time.Millisecond)
	})

	t.Run("keeps messages in order", func(t *testing.T) {
		got := subscribe(t, broker, group(), []string{topic, other}, accept)

		var want []string
		for i := range 20 {
			want = append(want, fmt.Sprint(i))
			publish(t, broker, []string{topic, other}[i%2], fmt.Sprint(i))
		}

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, want, got.data())
		}, wait, 50*time.Millisecond)
	})

	t.Run("redelivers a message that failed", func(t *testing.T) {
		got := subscribe(t, broker, group(), []string{topic}, func(d messaging.Delivery) error {
			if d.Attempt == 1 {
				return errors.New("database unavailable")
			}
			return nil
		})

		publish(t, broker, topic, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			deliveries := got.all()
			if assert.Len(c, deliveries, 2) {
				assert.Equal(c, 2, deliveries[1].Attempt)
				assert.Equal(c, "a", string(deliveries[1].Data))
			}
		}, wait, 50*time.Millisecond)
	})

	t.Run("never redelivers a rejected message", func(t *testing.T) {
		got := subscribe(t, broker, group(), []string{topic}, func(d messaging.Delivery) error {
			if string(d.Data) == "invalid" {
				return messaging.Reject(errors.New("invalid"))
			}
			return nil
		})

		publish(t, broker, topic, "invalid")
		publish(t, broker, topic, "a")

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"invalid", "a"}, got.data())
		}, wait, 50*time.Millisecond)
		time.Sleep(2 * time.Second)
		assert.Equal(t, []string{"invalid", "a"}, got.data())
	})

	t.Run("keeps a group's messages while it is stopped", func(t *testing.T) {
		name := group()
		subscription, err := broker.Subscribe(t.Context(), name, []string{topic}, func(context.Context, messaging.Delivery) error { return nil })
		require.NoError(t, err)
		subscription.Stop()

		publish(t, broker, topic, "a")
		got := subscribe(t, broker, name, []string{topic}, accept)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, []string{"a"}, got.data())
		}, wait, 50*time.Millisecond)
	})
}
