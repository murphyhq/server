//go:build integration

package integration

import (
	"net/http"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/internal/idempotency/store/redis"
	"github.com/murphyhq/server/internal/idempotency/storetest"
	"github.com/murphyhq/server/tests/testenv"
)

func TestRedisIdempotencyStore(t *testing.T) {
	store, err := redis.Open(testenv.RedisURL())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	storetest.Run(t, store)
}

func TestCreatesAreIdempotent(t *testing.T) {
	api, _ := startAPI(t)
	ctx := t.Context()
	key := &client.CreateServiceParams{IdempotencyKey: new(uuid.NewV4().String())}
	input := newService("checkout-api")

	first, err := api.CreateServiceWithResponse(ctx, key, input)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, first.StatusCode())

	retry, err := api.CreateServiceWithResponse(ctx, key, input)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, retry.StatusCode())
	assert.Equal(t, first.JSON201.Id, retry.JSON201.Id)

	listed, err := api.ListServicesWithResponse(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, listed.JSON200.Items, 1)

	different, err := api.CreateServiceWithResponse(ctx, key, newService("payments-api"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, different.StatusCode())
}
