package redis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/redis"
)

func TestOpenNeverRevealsTheURL(t *testing.T) {
	_, err := redis.Open(redis.Config{URL: "redis://:s3cret-password@localhost:not-a-port/0"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret-password")
}
