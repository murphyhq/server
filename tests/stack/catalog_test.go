//go:build stack

package stack

import (
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/murphytest"
)

func TestServicesAndTheirIntegrations(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	input := client.ServiceInput{Name: m.Name("checkout-api"), EscalationPolicy: client.Reference{Id: m.Policy().ID}}

	created, err := m.API.CreateServiceWithResponse(ctx, nil, input)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	service := created.JSON201
	assert.Equal(t, client.High, service.Urgency)
	assert.Equal(t, client.ServiceStatusActive, service.Status)

	fetched, err := m.API.GetServiceWithResponse(ctx, service.Id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, fetched.StatusCode())
	assert.Equal(t, service, fetched.JSON200)

	integration, err := m.API.CreateIntegrationWithResponse(ctx, service.Id, nil, client.IntegrationCreate{Name: "alertmanager", Type: client.Alertmanager})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, integration.StatusCode())
	assert.Equal(t, int32(1), integration.JSON201.KeyVersion)

	duplicate, err := m.API.CreateServiceWithResponse(ctx, nil, input)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, duplicate.StatusCode(), "names are unique")

	deleted, err := m.API.DeleteServiceWithResponse(ctx, service.Id)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, deleted.StatusCode())
	gone, err := m.API.GetIntegrationWithResponse(ctx, integration.JSON201.Id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, gone.StatusCode(), "deleting a service deletes its integrations")
}

func TestARotatedKeyAndThePreviousOneAreBothAcceptedUntilTheCutoff(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()
	previous := service.Key()

	rotated, err := m.API.RotateIntegrationKeyWithResponse(t.Context(), service.IntegrationID, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rotated.StatusCode())
	assert.Equal(t, int32(2), rotated.JSON200.KeyVersion)
	require.NotNil(t, rotated.JSON200.PreviousKeyValidUntil)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), *rotated.JSON200.PreviousKeyValidUntil, time.Minute)
	assert.NotEqual(t, previous, rotated.JSON200.Key)
	assert.Equal(t, http.StatusAccepted, m.SendWithKey(murphytest.Generic("disk-full", "firing"), rotated.JSON200.Key))
	assert.Equal(t, http.StatusAccepted, m.SendWithKey(murphytest.Generic("disk-full", "firing"), previous))

	missing, err := m.API.RotateIntegrationKeyWithResponse(t.Context(), uuid.NewV7().String(), nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, missing.StatusCode())
}

func TestServicesAreListedNewestFirst(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	policy := m.Policy()
	var newestFirst []string
	for range 3 {
		created, err := m.API.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: m.Name("service"), EscalationPolicy: client.Reference{Id: policy.ID}})
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, created.StatusCode())
		newestFirst = append([]string{created.JSON201.Id}, newestFirst...)
	}

	limit := 2
	first, err := m.API.ListServicesWithResponse(ctx, &client.ListServicesParams{Limit: &limit})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, first.StatusCode())
	require.NotEmpty(t, first.JSON200.NextCursor)
	second, err := m.API.ListServicesWithResponse(ctx, &client.ListServicesParams{Limit: &limit, Cursor: &first.JSON200.NextCursor})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, second.StatusCode())

	var listed []string
	for _, service := range append(first.JSON200.Items, second.JSON200.Items...) {
		listed = append(listed, service.Id)
	}
	assert.Equal(t, newestFirst, listed[:3])
}

func TestInvalidServiceRequestsAreRefused(t *testing.T) {
	m := murphytest.New(t)
	medium := client.Urgency("medium")

	invalid, err := m.API.CreateServiceWithResponse(t.Context(), nil, client.ServiceInput{Urgency: &medium})

	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, invalid.StatusCode())
	for _, field := range []string{"name", "escalation_policy_id", "urgency"} {
		assert.Contains(t, *invalid.ApplicationproblemJSON400.Detail, field)
	}
	resp := m.Raw(http.MethodGet, "/v1/services?status=active")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "unknown parameters are refused")
}

func TestMissingResourcesAndRoutesAnswerWithProblems(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()

	missing, err := m.API.CreateIntegrationWithResponse(ctx, uuid.NewV7().String(), nil, client.IntegrationCreate{Name: "x", Type: client.Generic})
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, missing.StatusCode())
	assert.Equal(t, "service not found", *missing.ApplicationproblemJSON404.Detail)

	for _, request := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/v1/services/", http.StatusNotFound},
		{http.MethodGet, "/v1/services//integrations", http.StatusNotFound},
		{http.MethodDelete, "/v1/services", http.StatusMethodNotAllowed},
	} {
		resp := m.Raw(request.method, request.path)
		assert.Equal(t, request.status, resp.StatusCode, request.path)
		assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"), request.path)
	}
}

func TestADisabledServiceRefusesAlerts(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()
	service.Disable()

	require.Equal(t, http.StatusAccepted, service.Send(murphytest.Generic("disk-full", "firing")))

	service.StaysWithoutIncidents()
}

func TestAServicesUrgencyCarriesIntoItsIncidents(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service(murphytest.Urgency(client.Low))

	incident := service.Fire("disk-full")

	assert.Equal(t, client.Low, incident.Get().Urgency)
}
