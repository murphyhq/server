package mysql_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/mysql"
)

func TestOpenNeverRevealsTheURL(t *testing.T) {
	_, err := mysql.Open(t.Context(), mysql.Config{URL: "mysql://murphy:s3cret-password@localhost:not-a-port/catalog"})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret-password")
}
