//go:build stack

package stack

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/services/catalog"
	"github.com/murphyhq/server/tests/murphytest"
)

func basic(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestAKeyWorksAsABearerTokenOrABasicPassword(t *testing.T) {
	m := murphytest.New(t)
	for name, authorization := range map[string]func(key string) string{
		"bearer": func(key string) string { return "Bearer " + key },
		"basic":  func(key string) string { return basic("murphy", key) },
	} {
		t.Run(name, func(t *testing.T) {
			service := m.Service()

			status, body := m.SendWithAuthorization(murphytest.Generic("disk-full", "firing"), authorization(service.Key()))

			require.Equal(t, http.StatusAccepted, status)
			assert.NotEmpty(t, body["id"])
			service.IncidentFor("disk-full")
		})
	}
}

func TestIngestRefusesRequestsWithoutAValidKey(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()
	other, err := catalog.ParseKeyring("9:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	require.NoError(t, err)
	forged, err := other.Sign(&murphy.Integration{Id: service.IntegrationID, ServiceId: service.ID, KeyVersion: 1})
	require.NoError(t, err)

	for name, authorization := range map[string]string{
		"missing":                "",
		"key without scheme":     service.Key(),
		"signed by another":      "Bearer " + forged,
		"basic with other user":  basic("admin", service.Key()),
		"basic with wrong value": basic("murphy", "nope"),
	} {
		t.Run(name, func(t *testing.T) {
			status, body := m.SendWithAuthorization(murphytest.Generic("disk-full", "firing"), authorization)

			assert.Equal(t, http.StatusUnauthorized, status)
			assert.Equal(t, "invalid integration key", body["detail"])
		})
	}
}

func TestIngestRefusesInvalidAlerts(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()

	for name, body := range map[string]string{
		"no dedup key":   `{"status": "firing", "summary": "s"}`,
		"unknown status": `{"dedup_key": "k", "status": "burning", "summary": "s"}`,
		"no summary":     `{"dedup_key": "k", "status": "resolved"}`,
	} {
		t.Run(name, func(t *testing.T) {
			status := service.Send(murphytest.Payload{Path: "/v1/alerts", ContentType: "application/json", Body: []byte(body)})
			assert.Equal(t, http.StatusBadRequest, status)
		})
	}
}

func TestEachFormatOpensAndResolvesAnIncident(t *testing.T) {
	m := murphytest.New(t)
	started := time.Now().Add(-time.Minute).UTC()
	alarm := "arn:aws:cloudwatch:eu-west-1:123456789012:alarm:disk-full"
	for name, format := range map[string]struct {
		kind              client.IntegrationType
		firing, resolving murphytest.Payload
	}{
		"alertmanager": {client.Alertmanager,
			murphytest.Alertmanager("group-1", murphytest.GroupedAlert{Fingerprint: "a", Status: "firing", StartsAt: started, Summary: "Disk almost full"}),
			murphytest.Alertmanager("group-1", murphytest.GroupedAlert{Fingerprint: "a", Status: "resolved", StartsAt: started, Summary: "Disk almost full"})},
		"grafana": {client.Grafana,
			murphytest.Grafana("group-1", murphytest.GroupedAlert{Fingerprint: "a", Status: "firing", StartsAt: started, Summary: "Disk almost full"}),
			murphytest.Grafana("group-1", murphytest.GroupedAlert{Fingerprint: "a", Status: "resolved", StartsAt: started, Summary: "Disk almost full"})},
		"datadog":      {client.Datadog, murphytest.Datadog("monitor-1", "cycle-1", "Triggered"), murphytest.Datadog("monitor-1", "cycle-1", "Recovered")},
		"cloudwatch":   {client.Cloudwatch, murphytest.CloudWatch(alarm, "ALARM"), murphytest.CloudWatch(alarm, "OK")},
		"google cloud": {client.GoogleCloud, murphytest.GoogleCloud("incident-1", "open"), murphytest.GoogleCloud("incident-1", "closed")},
	} {
		t.Run(name, func(t *testing.T) {
			service := m.Service(murphytest.Receives(format.kind))

			require.Equal(t, http.StatusAccepted, service.Send(format.firing))
			incident := service.WaitForIncident()
			incident.WaitFor(murphytest.Status(client.IncidentStatusTriggered))
			service.SendRecovery(format.resolving)

			incident.WaitFor(murphytest.Status(client.IncidentStatusResolved))
		})
	}
}

func TestAGroupStaysOpenUntilEveryAlertInItResolves(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service(murphytest.Receives(client.Alertmanager))
	started := time.Now().Add(-time.Minute).UTC()
	db1 := murphytest.GroupedAlert{Fingerprint: "db-1", Status: "firing", StartsAt: started, Summary: "Disk almost full"}
	db2 := murphytest.GroupedAlert{Fingerprint: "db-2", Status: "firing", StartsAt: started, Summary: "Disk almost full"}

	status, body := m.SendWithAuthorization(murphytest.Alertmanager("disk", db1, db2), "Bearer "+service.Key())
	require.Equal(t, http.StatusAccepted, status)
	assert.Len(t, body["ids"], 2, "one alert per instance")
	incident := service.WaitForIncident()
	incident.WaitFor(murphytest.Alerts(2))

	db1.Status = "resolved"
	require.Equal(t, http.StatusAccepted, service.Send(murphytest.Alertmanager("disk", db1, db2)))
	incident.WaitFor(murphytest.Alerts(4), murphytest.Status(client.IncidentStatusTriggered))

	db2.Status = "resolved"
	require.Equal(t, http.StatusAccepted, service.Send(murphytest.Alertmanager("disk", db1, db2)))
	incident.WaitFor(murphytest.Status(client.IncidentStatusResolved))
}

func TestAlertsInAnotherFormatThanTheIntegrationOpenNothing(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service(murphytest.Receives(client.Grafana))

	require.Equal(t, http.StatusAccepted, service.Send(murphytest.Datadog("monitor-1", "cycle-1", "Triggered")))

	service.StaysWithoutIncidents()
}

func TestCloudWatchSubscriptionsAndStatesWithoutAnAlert(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service(murphytest.Receives(client.Cloudwatch))
	auth := basic("murphy", service.Key())

	status, _ := m.SendWithAuthorization(murphytest.SNSSubscriptionConfirmation("https://attacker.example.com/confirm"), auth)
	assert.Equal(t, http.StatusBadRequest, status, "a confirmation URL outside SNS is refused")

	status, body := m.SendWithAuthorization(murphytest.CloudWatch("arn:aws:cloudwatch:eu-west-1:123456789012:alarm:disk-full", "INSUFFICIENT_DATA"), auth)
	require.Equal(t, http.StatusAccepted, status)
	assert.Empty(t, body["ids"], "an insufficient-data state is not an alert")
}

func TestAnUnknownAlertSourceIsNotFound(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()

	status := service.Send(murphytest.Payload{Path: "/v1/alerts/zabbix", ContentType: "application/json", Body: []byte("{}")})

	assert.Equal(t, http.StatusNotFound, status)
}
