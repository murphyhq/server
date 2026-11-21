package storetest

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/idempotency"
)

func Run(t *testing.T, store idempotency.Store) {
	t.Run("reserves a key only once", func(t *testing.T) {
		key := uuid.NewV4().String()
		existing, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, time.Minute)
		require.NoError(t, err)
		assert.Nil(t, existing)

		existing, err = store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "b"}, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, existing)
		assert.Equal(t, idempotency.Record{Fingerprint: "a"}, *existing)
	})

	t.Run("returns the completed response", func(t *testing.T) {
		key := uuid.NewV4().String()
		_, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, time.Minute)
		require.NoError(t, err)
		response := idempotency.Record{Fingerprint: "a", Status: 201, ContentType: "application/json", Body: []byte(`{"id":"1"}`)}
		require.NoError(t, store.Complete(t.Context(), key, response, time.Minute))

		existing, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, existing)
		assert.Equal(t, response, *existing)
	})

	t.Run("frees a released key", func(t *testing.T) {
		key := uuid.NewV4().String()
		_, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, time.Minute)
		require.NoError(t, err)
		require.NoError(t, store.Release(t.Context(), key))

		existing, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "b"}, time.Minute)
		require.NoError(t, err)
		assert.Nil(t, existing)
	})

	t.Run("keeps a completed response after its lease", func(t *testing.T) {
		key := uuid.NewV4().String()
		_, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, 200*time.Millisecond)
		require.NoError(t, err)
		response := idempotency.Record{Fingerprint: "a", Status: 201}
		require.NoError(t, store.Complete(t.Context(), key, response, time.Minute))

		time.Sleep(500 * time.Millisecond)
		existing, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, time.Minute)
		require.NoError(t, err)
		require.NotNil(t, existing)
		assert.Equal(t, response, *existing)
	})

	t.Run("frees an abandoned reservation after its lease", func(t *testing.T) {
		key := uuid.NewV4().String()
		_, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "a"}, 200*time.Millisecond)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			existing, err := store.Reserve(t.Context(), key, idempotency.Record{Fingerprint: "b"}, time.Minute)
			return err == nil && existing == nil
		}, 5*time.Second, 50*time.Millisecond)
	})
}
