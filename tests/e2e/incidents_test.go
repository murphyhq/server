//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
)

const settle = 30 * time.Second

func url(variable, fallback string) string {
	if value := os.Getenv(variable); value != "" {
		return value
	}
	return fallback
}

var (
	apiURL    = url("MURPHY_TEST_API_URL", "http://localhost:8080")
	ingestURL = url("MURPHY_TEST_INGEST_URL", "http://localhost:8081")
)

func setup(t *testing.T) (*client.ClientWithResponses, string, string) {
	t.Helper()
	ctx := t.Context()
	api, err := client.NewClientWithResponses(apiURL)
	require.NoError(t, err)

	service, err := api.CreateServiceWithResponse(ctx, nil, client.ServiceInput{
		Name:               "e2e-" + uuid.NewV4().String()[:8],
		TeamId:             uuid.NewV7().String(),
		EscalationPolicyId: uuid.NewV7().String(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, service.StatusCode())
	integration, err := api.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: "e2e", Type: client.Generic})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, integration.StatusCode())
	return api, service.JSON201.Id, integration.JSON201.Key
}

func send(t *testing.T, key, dedupKey, status string) int {
	t.Helper()
	body, err := json.Marshal(map[string]string{"dedup_key": dedupKey, "status": status, "summary": dedupKey + " is " + status})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ingestURL+"/v1/alerts", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func incidentOf(t *testing.T, api *client.ClientWithResponses, serviceID string) *client.IncidentWithTimeline {
	t.Helper()
	list, err := api.ListIncidentsWithResponse(t.Context(), nil)
	require.NoError(t, err)
	for _, incident := range list.JSON200.Items {
		if incident.ServiceId == serviceID {
			got, err := api.GetIncidentWithResponse(t.Context(), incident.Id)
			require.NoError(t, err)
			return got.JSON200
		}
	}
	return nil
}

func TestAnAlertOpensAnIncidentThatItsResolveCloses(t *testing.T) {
	api, serviceID, key := setup(t)

	require.Equal(t, http.StatusAccepted, send(t, key, "disk-full", "firing"))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		incident := incidentOf(t, api, serviceID)
		if assert.NotNil(c, incident) {
			assert.Equal(c, client.IncidentStatusTriggered, incident.Status)
		}
	}, settle, 250*time.Millisecond)

	require.Equal(t, http.StatusAccepted, send(t, key, "disk-full", "firing"))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Len(c, incidentOf(t, api, serviceID).Timeline, 2)
	}, settle, 250*time.Millisecond)

	time.Sleep(1100 * time.Millisecond)
	require.Equal(t, http.StatusAccepted, send(t, key, "disk-full", "resolved"))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		incident := incidentOf(t, api, serviceID)
		assert.Equal(c, client.IncidentStatusResolved, incident.Status)
		assert.Len(c, incident.Timeline, 3)
	}, settle, 250*time.Millisecond)
}

func TestIngestRefusesAKeyMurphyDidNotSign(t *testing.T) {
	assert.Equal(t, http.StatusUnauthorized, send(t, "murphy_ik_invalid.key", "disk-full", "firing"))
}
