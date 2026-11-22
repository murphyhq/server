//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

var (
	grafanaURL       = url("MURPHY_TEST_GRAFANA_URL", "http://localhost:3000")
	ingestForGrafana = url("MURPHY_TEST_GRAFANA_INGEST_URL", "http://ingest:8080")
)

type grafana struct {
	t *testing.T
}

func (g grafana) do(method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, grafanaURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("admin", "admin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Disable-Provenance", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s %s", method, path, resp.Status, data)
	}
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out, nil
}

func (g grafana) must(method, path string, body any) map[string]any {
	g.t.Helper()
	out, err := g.do(method, path, body)
	require.NoError(g.t, err)
	return out
}

func firingRule(uid, run, expr string) map[string]any {
	return map[string]any{
		"uid": uid, "title": "e2e " + run, "folderUID": "e2e", "ruleGroup": run, "condition": "C", "for": "0s",
		"noDataState": "OK", "execErrState": "Error", "labels": map[string]string{"e2e_run": run},
		"annotations": map[string]string{"summary": "End-to-end test " + run},
		"data": []map[string]any{
			{"refId": "A", "relativeTimeRange": map[string]int{"from": 60, "to": 0}, "datasourceUid": "prometheus",
				"model": map[string]any{"refId": "A", "expr": expr, "instant": true}},
			{"refId": "C", "datasourceUid": "__expr__",
				"model": map[string]any{"refId": "C", "type": "threshold", "expression": "A",
					"conditions": []map[string]any{{"evaluator": map[string]any{"type": "gt", "params": []int{0}}}}}},
		},
	}
}

func TestGrafanaPagesMurphyAndResolvesTheIncident(t *testing.T) {
	ctx := t.Context()
	api, err := client.NewClientWithResponses(apiURL)
	require.NoError(t, err)
	service, err := api.CreateServiceWithResponse(ctx, nil, client.ServiceInput{
		Name: "e2e-grafana-" + uuid.NewV4().String()[:8], TeamId: uuid.NewV7().String(), EscalationPolicyId: uuid.NewV7().String(),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, service.StatusCode())
	integration, err := api.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: "grafana", Type: client.Grafana})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, integration.StatusCode())

	g := grafana{t: t}
	run := "e2e" + uuid.NewV4().String()[:8]
	contact := "murphy-" + run
	_, _ = g.do(http.MethodPost, "/api/folders", map[string]string{"uid": "e2e", "title": "End-to-end tests"})
	g.must(http.MethodPost, "/api/v1/provisioning/contact-points", map[string]any{
		"uid": contact, "name": contact, "type": "webhook",
		"settings": map[string]any{"url": ingestForGrafana + "/v1/alerts/grafana", "authorization_scheme": "Bearer", "authorization_credentials": integration.JSON201.Key},
	})
	policies := g.must(http.MethodGet, "/api/v1/provisioning/policies", nil)
	original, err := json.Marshal(policies)
	require.NoError(t, err)
	routes, _ := policies["routes"].([]any)
	policies["routes"] = append([]any{map[string]any{
		"receiver": contact, "object_matchers": [][]string{{"e2e_run", "=", run}},
		"group_by": []string{"alertname"}, "group_wait": "1s", "group_interval": "1s", "repeat_interval": "1h",
	}}, routes...)
	g.must(http.MethodPut, "/api/v1/provisioning/policies", policies)
	t.Cleanup(func() {
		var restore map[string]any
		_ = json.Unmarshal(original, &restore)
		_, _ = g.do(http.MethodPut, "/api/v1/provisioning/policies", restore)
		_, _ = g.do(http.MethodDelete, "/api/v1/provisioning/folder/e2e/rule-groups/"+run, nil)
		_, _ = g.do(http.MethodDelete, "/api/v1/provisioning/contact-points/"+contact, nil)
	})

	rule := "rule-" + run
	g.must(http.MethodPut, "/api/v1/provisioning/folder/e2e/rule-groups/"+run, map[string]any{
		"title": run, "folderUid": "e2e", "interval": 10, "rules": []map[string]any{firingRule(rule, run, "vector(1)")},
	})

	var incidentID string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		incident := incidentOf(t, api, service.JSON201.Id)
		if assert.NotNil(c, incident, "no incident yet") {
			incidentID = incident.Id
			assert.Equal(c, client.IncidentStatusTriggered, incident.Status)
		}
	}, 3*time.Minute, 2*time.Second)

	g.must(http.MethodPut, "/api/v1/provisioning/folder/e2e/rule-groups/"+run, map[string]any{
		"title": run, "folderUid": "e2e", "interval": 10, "rules": []map[string]any{firingRule(rule, run, "vector(0)")},
	})
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		incident := incidentOf(t, api, service.JSON201.Id)
		assert.Equal(c, client.IncidentStatusResolved, incident.Status, fmt.Sprintf("incident %s not resolved yet", incidentID))
	}, 3*time.Minute, 2*time.Second)
}
