package storetest

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/ratelimit"
)

func Run(t *testing.T, counter ratelimit.Counter) {
	t.Run("counts hits under one key", func(t *testing.T) {
		key, other := uuid.NewV4().String(), uuid.NewV4().String()

		_, err := counter.Hit(t.Context(), key, time.Minute)
		require.NoError(t, err)
		second, err := counter.Hit(t.Context(), key, time.Minute)
		require.NoError(t, err)
		counted, err := counter.Count(t.Context(), key)
		require.NoError(t, err)
		none, err := counter.Count(t.Context(), other)
		require.NoError(t, err)

		assert.Equal(t, int64(2), second)
		assert.Equal(t, int64(2), counted)
		assert.Zero(t, none)
	})

	t.Run("forgets hits once their window ends, however many came after the first", func(t *testing.T) {
		key := uuid.NewV4().String()
		_, err := counter.Hit(t.Context(), key, time.Second)
		require.NoError(t, err)
		_, err = counter.Hit(t.Context(), key, time.Hour)
		require.NoError(t, err)

		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			counted, err := counter.Count(t.Context(), key)
			assert.NoError(c, err)
			assert.Zero(c, counted)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("a reset forgets every hit", func(t *testing.T) {
		key := uuid.NewV4().String()
		_, err := counter.Hit(t.Context(), key, time.Minute)
		require.NoError(t, err)

		require.NoError(t, counter.Reset(t.Context(), key))

		counted, err := counter.Count(t.Context(), key)
		require.NoError(t, err)
		assert.Zero(t, counted)
	})
}
