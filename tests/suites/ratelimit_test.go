//go:build integration

package suites

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/ratelimit/store/redis"
	"github.com/murphyhq/server/internal/ratelimit/storetest"
	"github.com/murphyhq/server/tests/testenv"
)

func TestRedisRateLimitStore(t *testing.T) {
	store, err := redis.Open(t.Context(), testenv.RedisURL())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	storetest.Run(t, store)
}
