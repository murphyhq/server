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

func TestOpenRefusesInvalidPoolSettingsWithoutRevealingTheURL(t *testing.T) {
	for _, query := range []string{"pool_max_conns=0", "pool_max_conns=many", "pool_max_conn_lifetime=-1m", "pool_max_conn_idle_time=soon"} {
		_, err := mysql.Open(t.Context(), mysql.Config{URL: "mysql://murphy:s3cret-password@localhost:3306/catalog?" + query})

		require.Error(t, err, query)
		assert.NotContains(t, err.Error(), "s3cret-password", query)
	}
}
