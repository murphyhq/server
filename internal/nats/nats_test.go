package nats_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/nats"
)

func TestOpenNeverRevealsTheURL(t *testing.T) {
	for _, url := range []string{"nats://murphy:s3cret-password@localhost:1", "nats://murphy:s3cret-password@local host"} {
		_, _, err := nats.Open(nats.Config{URL: url})

		require.Error(t, err)
		assert.NotContains(t, err.Error(), "s3cret-password")
	}
}
