//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/services/catalog"
	"github.com/murphyhq/server/tests/testenv"
)

func TestCatalogServicesAndIntegrations(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		ctx := t.Context()

		created, err := api.CreateServiceWithResponse(ctx, nil, newService("checkout-api"))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, created.StatusCode())
		service := created.JSON201
		assert.Equal(t, client.High, service.Urgency)
		assert.Equal(t, client.ServiceStatusActive, service.Status)

		fetched, err := api.GetServiceWithResponse(ctx, service.Id)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, fetched.StatusCode())
		assert.Equal(t, service, fetched.JSON200)

		integration, err := api.CreateIntegrationWithResponse(ctx, service.Id, nil, client.IntegrationCreate{
			Name: "alertmanager",
			Type: client.Alertmanager,
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, integration.StatusCode())
		assert.Equal(t, int32(1), integration.JSON201.KeyVersion)

		duplicate, err := api.CreateServiceWithResponse(ctx, nil, newService("checkout-api"))
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, duplicate.StatusCode())

		deleted, err := api.DeleteServiceWithResponse(ctx, service.Id)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, deleted.StatusCode())

		gone, err := api.GetIntegrationWithResponse(ctx, integration.JSON201.Id)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, gone.StatusCode())
	})
}

func TestIntegrationKeysAreIssuedAndRotated(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		ctx := t.Context()
		keys, err := catalog.ParseKeyring(testenv.IntegrationKeySecrets)
		require.NoError(t, err)

		service, err := api.CreateServiceWithResponse(ctx, nil, newService("checkout-api"))
		require.NoError(t, err)
		created, err := api.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: "grafana", Type: client.Grafana})
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, created.StatusCode())

		first, err := keys.Verify(created.JSON201.Key)
		require.NoError(t, err)
		assert.Equal(t, created.JSON201.Id, first.GetIntegrationId())
		assert.Equal(t, service.JSON201.Id, first.GetServiceId())
		assert.Equal(t, int32(1), first.GetKeyVersion())

		rotated, err := api.RotateIntegrationKeyWithResponse(ctx, created.JSON201.Id, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rotated.StatusCode())
		assert.Equal(t, int32(2), rotated.JSON200.KeyVersion)
		require.NotNil(t, rotated.JSON200.PreviousKeyValidUntil)
		assert.WithinDuration(t, time.Now().Add(24*time.Hour), *rotated.JSON200.PreviousKeyValidUntil, time.Minute)

		second, err := keys.Verify(rotated.JSON200.Key)
		require.NoError(t, err)
		assert.Equal(t, int32(2), second.GetKeyVersion())
		assert.NotEqual(t, created.JSON201.Key, rotated.JSON200.Key)

		missing, err := api.RotateIntegrationKeyWithResponse(ctx, uuid.NewV7().String(), nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, missing.StatusCode())
	})
}

func TestCatalogListsServicesNewestFirst(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		ctx := t.Context()

		var ids []string
		for _, name := range []string{"first", "second", "third"} {
			created, err := api.CreateServiceWithResponse(ctx, nil, newService(name))
			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, created.StatusCode())
			ids = append([]string{created.JSON201.Id}, ids...)
		}

		limit := 2
		first, err := api.ListServicesWithResponse(ctx, &client.ListServicesParams{Limit: &limit})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, first.StatusCode())
		require.NotEmpty(t, first.JSON200.NextCursor)

		second, err := api.ListServicesWithResponse(ctx, &client.ListServicesParams{Limit: &limit, Cursor: &first.JSON200.NextCursor})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, second.StatusCode())
		assert.Empty(t, second.JSON200.NextCursor)

		var listed []string
		for _, service := range append(first.JSON200.Items, second.JSON200.Items...) {
			listed = append(listed, service.Id)
		}
		assert.Equal(t, ids, listed)
	})
}

func TestCatalogRejectsInvalidRequests(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, base := testenv.API(t, engine)
		ctx := t.Context()

		medium := client.Urgency("medium")
		invalid, err := api.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Urgency: &medium})
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, invalid.StatusCode())
		for _, field := range []string{"name", "escalation_policy_id", "urgency"} {
			assert.Contains(t, *invalid.ApplicationproblemJSON400.Detail, field)
		}

		resp, err := http.Get(base + "/v1/services?status=active")
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestCatalogNotFoundResponses(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, base := testenv.API(t, engine)
		ctx := t.Context()

		missing, err := api.CreateIntegrationWithResponse(ctx, uuid.NewV7().String(), nil, client.IntegrationCreate{Name: "x", Type: client.Generic})
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
			req, err := http.NewRequestWithContext(ctx, request.method, base+request.path, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, request.status, resp.StatusCode, request.path)
			assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"), request.path)
		}
	})
}

func newService(name string) client.ServiceInput {
	return client.ServiceInput{
		Name:             name,
		EscalationPolicy: client.Reference{Id: uuid.NewV7().String()},
	}
}
