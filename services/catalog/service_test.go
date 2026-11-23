package catalog_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/catalog"
)

func TestCreateServiceAppliesDefaults(t *testing.T) {
	repo := NewMockRepository(t)
	var stored *murphy.Service
	repo.EXPECT().CreateService(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, service *murphy.Service) error {
		stored = service
		return nil
	})

	service, err := catalog.New(repo, nil).CreateService(t.Context(), validService())
	require.NoError(t, err)

	assert.Equal(t, murphy.Urgency_URGENCY_HIGH, service.GetUrgency())
	assert.Equal(t, murphy.ServiceStatus_SERVICE_STATUS_ACTIVE, service.GetStatus())
	assert.NotEmpty(t, service.GetId())
	assert.NotNil(t, service.GetCreatedAt())
	assert.True(t, proto.Equal(service, stored))
}

func TestCreateServiceRejectsInvalidInput(t *testing.T) {
	repo := NewMockRepository(t)

	_, err := catalog.New(repo, nil).CreateService(t.Context(), &murphy.Service{EscalationPolicyId: "not-a-uuid"})

	require.ErrorIs(t, err, murphy.ErrInvalid)
	for _, field := range []string{"name", "escalation_policy_id"} {
		assert.ErrorContains(t, err, field)
	}
}

func TestUpdateServiceKeepsIdentityAndUnsetFields(t *testing.T) {
	repo := NewMockRepository(t)
	current := validService()
	current.Id = murphy.NewID()
	current.Urgency = murphy.Urgency_URGENCY_LOW
	current.Status = murphy.ServiceStatus_SERVICE_STATUS_ACTIVE
	current.CreatedAt = murphy.Now()
	repo.EXPECT().GetService(mock.Anything, current.GetId()).Return(current, nil)
	var stored *murphy.Service
	repo.EXPECT().UpdateService(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, service *murphy.Service) error {
		stored = service
		return nil
	})

	update := validService()
	update.Id = current.GetId()
	update.Name = "payments-api"
	updated, err := catalog.New(repo, nil).UpdateService(t.Context(), update)
	require.NoError(t, err)

	assert.Equal(t, "payments-api", updated.GetName())
	assert.Equal(t, murphy.Urgency_URGENCY_LOW, updated.GetUrgency())
	assert.True(t, proto.Equal(current.GetCreatedAt(), updated.GetCreatedAt()))
	assert.True(t, proto.Equal(updated, stored))
}

func TestCreateIntegrationRequiresExistingService(t *testing.T) {
	repo := NewMockRepository(t)
	serviceID := murphy.NewID()
	repo.EXPECT().GetService(mock.Anything, serviceID).Return(nil, &murphy.NotFoundError{Entity: "service"})

	_, _, err := catalog.New(repo, nil).CreateIntegration(t.Context(), &murphy.Integration{
		ServiceId: serviceID,
		Name:      "alertmanager",
		Type:      murphy.IntegrationType_INTEGRATION_TYPE_ALERTMANAGER,
	})

	require.ErrorIs(t, err, murphy.ErrNotFound)
	assert.ErrorContains(t, err, "service not found")
}

func validService() *murphy.Service {
	return &murphy.Service{
		Name:               "checkout-api",
		EscalationPolicyId: uuid.NewV7().String(),
	}
}
