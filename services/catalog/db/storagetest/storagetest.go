package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/catalog"
)

const maxID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

func Run(t *testing.T, open func(t *testing.T) catalog.Repository) {
	t.Run("stores and reads back a service", func(t *testing.T) {
		repo := open(t)
		service := newService("checkout-api")

		require.NoError(t, repo.CreateService(t.Context(), service))
		got, err := repo.GetService(t.Context(), service.GetId())

		require.NoError(t, err)
		assert.Equal(t, service.GetName(), got.GetName())
		assert.Equal(t, service.GetUrgency(), got.GetUrgency())
		assert.Equal(t, service.GetCreatedAt().AsTime(), got.GetCreatedAt().AsTime())
	})

	t.Run("refuses a second service with the same name", func(t *testing.T) {
		repo := open(t)
		require.NoError(t, repo.CreateService(t.Context(), newService("checkout-api")))

		err := repo.CreateService(t.Context(), newService("checkout-api"))

		assert.ErrorIs(t, err, murphy.ErrConflict)
	})

	t.Run("treats names as case sensitive", func(t *testing.T) {
		repo := open(t)
		require.NoError(t, repo.CreateService(t.Context(), newService("checkout-api")))

		assert.NoError(t, repo.CreateService(t.Context(), newService("Checkout-API")))
	})

	t.Run("lists services newest first from a cursor", func(t *testing.T) {
		repo := open(t)
		first, second := newService("first"), newService("second")
		require.NoError(t, repo.CreateService(t.Context(), first))
		require.NoError(t, repo.CreateService(t.Context(), second))

		all, err := repo.ListServices(t.Context(), maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.GetId(), first.GetId()}, serviceIDs(all))

		page, err := repo.ListServices(t.Context(), second.GetId(), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.GetId()}, serviceIDs(page))
	})

	t.Run("updates a service, even to the same values", func(t *testing.T) {
		repo := open(t)
		service := newService("checkout-api")
		require.NoError(t, repo.CreateService(t.Context(), service))

		require.NoError(t, repo.UpdateService(t.Context(), service))
		service.Name = "payments-api"
		require.NoError(t, repo.UpdateService(t.Context(), service))

		got, err := repo.GetService(t.Context(), service.GetId())
		require.NoError(t, err)
		assert.Equal(t, "payments-api", got.GetName())
	})

	t.Run("tells whether any service pages with a policy", func(t *testing.T) {
		repo := open(t)
		service := newService("checkout-api")
		require.NoError(t, repo.CreateService(t.Context(), service))

		used, err := repo.PolicyInUse(t.Context(), service.GetEscalationPolicyId())
		require.NoError(t, err)
		assert.True(t, used)
		unused, err := repo.PolicyInUse(t.Context(), murphy.NewID())
		require.NoError(t, err)
		assert.False(t, unused)
	})

	t.Run("reports missing services and integrations", func(t *testing.T) {
		repo := open(t)
		_, err := repo.GetService(t.Context(), murphy.NewID())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		assert.ErrorIs(t, repo.UpdateService(t.Context(), newService("ghost")), murphy.ErrNotFound)
		assert.ErrorIs(t, repo.DeleteService(t.Context(), murphy.NewID()), murphy.ErrNotFound)
		_, err = repo.GetIntegration(t.Context(), murphy.NewID())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		_, err = repo.RotateIntegrationKey(t.Context(), murphy.NewID(), time.Now(), time.Now())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("lists a service's integrations", func(t *testing.T) {
		repo := open(t)
		service, other := newService("checkout-api"), newService("payments-api")
		require.NoError(t, repo.CreateService(t.Context(), service))
		require.NoError(t, repo.CreateService(t.Context(), other))
		mine := newIntegration(service.GetId())
		require.NoError(t, repo.CreateIntegration(t.Context(), mine))
		require.NoError(t, repo.CreateIntegration(t.Context(), newIntegration(other.GetId())))

		list, err := repo.ListIntegrations(t.Context(), service.GetId(), maxID, 10)

		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, mine.GetId(), list[0].GetId())
	})

	t.Run("rotates an integration's key", func(t *testing.T) {
		repo := open(t)
		service := newService("checkout-api")
		require.NoError(t, repo.CreateService(t.Context(), service))
		integration := newIntegration(service.GetId())
		require.NoError(t, repo.CreateIntegration(t.Context(), integration))
		until := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Microsecond)

		rotated, err := repo.RotateIntegrationKey(t.Context(), integration.GetId(), until, time.Now())

		require.NoError(t, err)
		assert.Equal(t, int32(2), rotated.GetKeyVersion())
		require.NotNil(t, rotated.GetPreviousKeyValidUntil())
		assert.Equal(t, until, rotated.GetPreviousKeyValidUntil().AsTime())
	})

	t.Run("deletes a service with its integrations", func(t *testing.T) {
		repo := open(t)
		service := newService("checkout-api")
		require.NoError(t, repo.CreateService(t.Context(), service))
		integration := newIntegration(service.GetId())
		require.NoError(t, repo.CreateIntegration(t.Context(), integration))

		require.NoError(t, repo.DeleteService(t.Context(), service.GetId()))

		_, err := repo.GetIntegration(t.Context(), integration.GetId())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})
}

func newService(name string) *murphy.Service {
	now := murphy.Now()
	return &murphy.Service{
		Id: murphy.NewID(), Name: name, EscalationPolicyId: murphy.NewID(),
		Urgency: murphy.Urgency_URGENCY_HIGH, Status: murphy.ServiceStatus_SERVICE_STATUS_ACTIVE,
		CreatedAt: now, UpdatedAt: now,
	}
}

func newIntegration(serviceID string) *murphy.Integration {
	now := murphy.Now()
	return &murphy.Integration{
		Id: murphy.NewID(), ServiceId: serviceID, Name: "grafana", Type: murphy.IntegrationType_INTEGRATION_TYPE_GRAFANA,
		KeyVersion: 1, Status: murphy.IntegrationStatus_INTEGRATION_STATUS_ACTIVE, CreatedAt: now, UpdatedAt: now,
	}
}

func serviceIDs(list []*murphy.Service) []string {
	out := make([]string, len(list))
	for i, service := range list {
		out[i] = service.GetId()
	}
	return out
}
