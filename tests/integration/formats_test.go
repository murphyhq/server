//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/testenv"
)

func deliver(t *testing.T, stack testenv.Stack, authorization string, webhook testenv.Webhook) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, stack.IngestURL+webhook.Path, bytes.NewReader(webhook.Body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", webhook.ContentType)
	req.Header.Set("Authorization", authorization)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func incidentFor(t *testing.T, api *client.ClientWithResponses, serviceID string) *client.Incident {
	t.Helper()
	for _, incident := range incidentsOf(t, api) {
		if incident.ServiceId == serviceID {
			return &incident
		}
	}
	return nil
}

func eventually(t *testing.T, check func(c *assert.CollectT)) {
	t.Helper()
	require.EventuallyWithT(t, check, settle, 100*time.Millisecond)
}

func TestEachFormatOpensAndResolvesAnIncident(t *testing.T) {
	started := time.Now().Add(-time.Minute).UTC()
	for name, format := range map[string]struct {
		kind              client.IntegrationType
		firing, resolving testenv.Webhook
	}{
		"alertmanager": {client.Alertmanager,
			testenv.Alertmanager("group-1", testenv.GroupedAlert{Fingerprint: "a", Status: "firing", StartsAt: started, Summary: "Disk almost full"}),
			testenv.Alertmanager("group-1", testenv.GroupedAlert{Fingerprint: "a", Status: "resolved", StartsAt: started, Summary: "Disk almost full"})},
		"grafana": {client.Grafana,
			testenv.Grafana("group-1", testenv.GroupedAlert{Fingerprint: "a", Status: "firing", StartsAt: started, Summary: "Disk almost full"}),
			testenv.Grafana("group-1", testenv.GroupedAlert{Fingerprint: "a", Status: "resolved", StartsAt: started, Summary: "Disk almost full"})},
		"datadog":      {client.Datadog, testenv.Datadog("monitor-1", "cycle-1", "Triggered"), testenv.Datadog("monitor-1", "cycle-1", "Recovered")},
		"cloudwatch":   {client.Cloudwatch, testenv.CloudWatch("arn:aws:cloudwatch:eu-west-1:123456789012:alarm:disk-full", "ALARM"), testenv.CloudWatch("arn:aws:cloudwatch:eu-west-1:123456789012:alarm:disk-full", "OK")},
		"google cloud": {client.GoogleCloud, testenv.GoogleCloud("incident-1", "open"), testenv.GoogleCloud("incident-1", "closed")},
	} {
		t.Run(name, func(t *testing.T) {
			testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
				stack := testenv.Murphy(t, testenv.NATS(), engine)
				serviceID, _, key := integrationOf(t, stack.API, format.kind)

				status, _ := deliver(t, stack, "Bearer "+key, format.firing)
				require.Equal(t, http.StatusAccepted, status)
				eventually(t, func(c *assert.CollectT) {
					incident := incidentFor(t, stack.API, serviceID)
					if assert.NotNil(c, incident) {
						assert.Equal(c, client.IncidentStatusTriggered, incident.Status)
					}
				})

				time.Sleep(1100 * time.Millisecond)
				status, _ = deliver(t, stack, "Bearer "+key, format.resolving)
				require.Equal(t, http.StatusAccepted, status)
				eventually(t, func(c *assert.CollectT) {
					assert.Equal(c, client.IncidentStatusResolved, incidentFor(t, stack.API, serviceID).Status)
				})
			})
		})
	}
}

func TestAGroupStaysOpenUntilEveryAlertInItResolves(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		serviceID, _, key := integrationOf(t, stack.API, client.Alertmanager)
		started := time.Now().Add(-time.Minute).UTC()
		db1 := testenv.GroupedAlert{Fingerprint: "db-1", Status: "firing", StartsAt: started, Summary: "Disk almost full"}
		db2 := testenv.GroupedAlert{Fingerprint: "db-2", Status: "firing", StartsAt: started, Summary: "Disk almost full"}

		status, body := deliver(t, stack, "Bearer "+key, testenv.Alertmanager("disk", db1, db2))
		require.Equal(t, http.StatusAccepted, status)
		assert.Len(t, body["ids"], 2)
		eventually(t, func(c *assert.CollectT) {
			incident := incidentFor(t, stack.API, serviceID)
			if assert.NotNil(c, incident) {
				assert.Len(c, timelineTypes(t, stack.API, incident.Id), 2)
			}
		})

		db1.Status = "resolved"
		status, _ = deliver(t, stack, "Bearer "+key, testenv.Alertmanager("disk", db1, db2))
		require.Equal(t, http.StatusAccepted, status)
		eventually(t, func(c *assert.CollectT) {
			assert.Len(c, timelineTypes(t, stack.API, incidentFor(t, stack.API, serviceID).Id), 4)
		})
		assert.Equal(t, client.IncidentStatusTriggered, incidentFor(t, stack.API, serviceID).Status)

		db2.Status = "resolved"
		status, _ = deliver(t, stack, "Bearer "+key, testenv.Alertmanager("disk", db1, db2))
		require.Equal(t, http.StatusAccepted, status)
		eventually(t, func(c *assert.CollectT) {
			assert.Equal(c, client.IncidentStatusResolved, incidentFor(t, stack.API, serviceID).Status)
		})
	})
}

func TestAlertsInAnotherFormatThanTheIntegrationOpenNothing(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		serviceID, _, key := integrationOf(t, stack.API, client.Grafana)

		status, _ := deliver(t, stack, "Bearer "+key, testenv.Datadog("monitor-1", "cycle-1", "Triggered"))
		require.Equal(t, http.StatusAccepted, status)

		time.Sleep(2 * time.Second)
		assert.Nil(t, incidentFor(t, stack.API, serviceID))
	})
}

func TestCloudWatchSubscriptionsAndStatesWithoutAnAlert(t *testing.T) {
	stack := testenv.Murphy(t, testenv.NATS(), testenv.PostgresDatabase)
	serviceID, _, key := integrationOf(t, stack.API, client.Cloudwatch)
	auth := basic("murphy", key)

	status, _ := deliver(t, stack, auth, testenv.SNSSubscriptionConfirmation("https://attacker.example.com/confirm"))
	assert.Equal(t, http.StatusBadRequest, status, "a confirmation URL outside SNS is refused")

	status, body := deliver(t, stack, auth, testenv.CloudWatch("arn:aws:cloudwatch:eu-west-1:123456789012:alarm:disk-full", "INSUFFICIENT_DATA"))
	require.Equal(t, http.StatusAccepted, status)
	assert.Empty(t, body["ids"])
	time.Sleep(time.Second)
	assert.Nil(t, incidentFor(t, stack.API, serviceID))
}
