package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/postgres"
)

func TestOpenNeverRevealsTheURL(t *testing.T) {
	_, err := postgres.Open(t.Context(), postgres.Config{URL: "postgres://murphy:s3cret-password@localhost:not-a-port/catalog"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret-password")
}
