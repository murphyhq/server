package lifecycle_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/telemetry"
)

func TestAComponentRunsWithItsNameOnEverythingItDoes(t *testing.T) {
	var seen context.Context
	run := lifecycle.Component("engine", func(ctx context.Context) error {
		seen = ctx
		return nil
	})

	require.NoError(t, run(t.Context()))

	assert.Equal(t, "engine", telemetry.Component(seen))
	assert.Contains(t, telemetry.Fields(seen), telemetry.WithField("component", "engine"), "on its spans and log lines too")
}
