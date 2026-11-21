package catalog_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/catalog"
)

func TestCreateServiceAppliesDefaults(t *testing.T) {
	repo := newFakeRepository()
	service, err := catalog.New(repo).CreateService(t.Context(), validService())
	require.NoError(t, err)

	assert.Equal(t, murphy.Urgency_URGENCY_HIGH, service.GetUrgency())
	assert.Equal(t, murphy.ServiceStatus_SERVICE_STATUS_ACTIVE, service.GetStatus())
	assert.NotEmpty(t, service.GetId())
	assert.NotNil(t, service.GetCreatedAt())
	assert.True(t, proto.Equal(service, repo.services[service.GetId()]))
}

func TestCreateServiceRejectsInvalidInput(t *testing.T) {
	repo := newFakeRepository()
	_, err := catalog.New(repo).CreateService(t.Context(), &murphy.Service{TeamId: "not-a-uuid"})

	require.ErrorIs(t, err, murphy.ErrInvalid)
	for _, field := range []string{"name", "team_id", "escalation_policy_id"} {
		assert.ErrorContains(t, err, field)
	}
	assert.Empty(t, repo.services)
}

func TestUpdateServiceKeepsIdentityAndUnsetFields(t *testing.T) {
	repo := newFakeRepository()
	svc := catalog.New(repo)
	in := validService()
	in.Urgency = murphy.Urgency_URGENCY_LOW
	created, err := svc.CreateService(t.Context(), in)
	require.NoError(t, err)

	update := validService()
	update.Id = created.GetId()
	update.Name = "payments-api"
	updated, err := svc.UpdateService(t.Context(), update)
	require.NoError(t, err)

	assert.Equal(t, "payments-api", updated.GetName())
	assert.Equal(t, murphy.Urgency_URGENCY_LOW, updated.GetUrgency())
	assert.True(t, proto.Equal(created.GetCreatedAt(), updated.GetCreatedAt()))
	assert.True(t, proto.Equal(updated, repo.services[created.GetId()]))
}

func TestCreateIntegrationRequiresExistingService(t *testing.T) {
	repo := newFakeRepository()
	_, err := catalog.New(repo).CreateIntegration(t.Context(), &murphy.Integration{
		ServiceId: uuid.NewV7().String(),
		Name:      "alertmanager",
		Type:      murphy.IntegrationType_INTEGRATION_TYPE_ALERTMANAGER,
	})

	require.ErrorIs(t, err, murphy.ErrNotFound)
	assert.Empty(t, repo.integrations)
}

func validService() *murphy.Service {
	return &murphy.Service{
		Name:               "checkout-api",
		TeamId:             uuid.NewV7().String(),
		EscalationPolicyId: uuid.NewV7().String(),
	}
}

type fakeRepository struct {
	services     map[string]*murphy.Service
	integrations map[string]*murphy.Integration
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{services: map[string]*murphy.Service{}, integrations: map[string]*murphy.Integration{}}
}

func (f *fakeRepository) CreateService(_ context.Context, service *murphy.Service) error {
	f.services[service.GetId()] = service
	return nil
}

func (f *fakeRepository) GetService(_ context.Context, id string) (*murphy.Service, error) {
	service, ok := f.services[id]
	if !ok {
		return nil, murphy.ErrNotFound
	}
	return service, nil
}

func (f *fakeRepository) ListServices(context.Context, string, int) ([]*murphy.Service, error) {
	return nil, nil
}

func (f *fakeRepository) UpdateService(_ context.Context, service *murphy.Service) error {
	if _, ok := f.services[service.GetId()]; !ok {
		return murphy.ErrNotFound
	}
	f.services[service.GetId()] = service
	return nil
}

func (f *fakeRepository) DeleteService(_ context.Context, id string) error {
	delete(f.services, id)
	return nil
}

func (f *fakeRepository) CreateIntegration(_ context.Context, integration *murphy.Integration) error {
	f.integrations[integration.GetId()] = integration
	return nil
}

func (f *fakeRepository) GetIntegration(_ context.Context, id string) (*murphy.Integration, error) {
	integration, ok := f.integrations[id]
	if !ok {
		return nil, murphy.ErrNotFound
	}
	return integration, nil
}

func (f *fakeRepository) ListIntegrations(context.Context, string, string, int) ([]*murphy.Integration, error) {
	return nil, nil
}

func (f *fakeRepository) UpdateIntegration(_ context.Context, integration *murphy.Integration) error {
	f.integrations[integration.GetId()] = integration
	return nil
}

func (f *fakeRepository) DeleteIntegration(_ context.Context, id string) error {
	delete(f.integrations, id)
	return nil
}
