//go:build stack

package stack

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/murphytest"
)

func TestAlertsOpenJoinAndResolveIncidents(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()

	disk := service.Fire("disk-full")
	cpu := service.Fire("cpu-high")
	require.Equal(t, http.StatusAccepted, service.Send(murphytest.Generic("disk-full", "firing")))
	disk.WaitFor(murphytest.Entries(client.TimelineEntryTypeTriggered, client.TimelineEntryTypeAlerted))
	service.Recover("disk-full")

	resolved := disk.WaitFor(
		murphytest.Status(client.IncidentStatusResolved),
		murphytest.Entries(client.TimelineEntryTypeTriggered, client.TimelineEntryTypeAlerted, client.TimelineEntryTypeResolved),
	)
	assert.NotNil(t, resolved.ResolvedAt)
	assert.Equal(t, client.High, resolved.Urgency)
	open := cpu.Get()
	assert.Equal(t, client.IncidentStatusTriggered, open.Status)
	assert.NotEqual(t, resolved.Number, open.Number)
}

func TestConcurrentAlertsOpenOneIncident(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { assert.Equal(t, http.StatusAccepted, service.Send(murphytest.Generic("disk-full", "firing"))) })
	}
	wg.Wait()

	service.WaitForIncident().WaitFor(murphytest.Alerts(10))
	assert.Len(t, service.Incidents(), 1)
}

func TestAResolveRacingARefireNeverClosesTheIncident(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()
	service.Fire("disk-full")

	var wg sync.WaitGroup
	wg.Go(func() {
		assert.Equal(t, http.StatusAccepted, service.Send(murphytest.Generic("disk-full", "resolved")))
	})
	wg.Go(func() { assert.Equal(t, http.StatusAccepted, service.Send(murphytest.Generic("disk-full", "firing"))) })
	wg.Wait()

	service.WaitFor(murphytest.AlertsAcrossIncidents(3), murphytest.OpenIncidents(1))
}

func TestAlertsForADisabledIntegrationOpenNothing(t *testing.T) {
	m := murphytest.New(t)
	service := m.Service()
	disabled := client.IntegrationStatusDisabled
	updated, err := m.API.UpdateIntegrationWithResponse(t.Context(), service.IntegrationID, client.IntegrationUpdate{Name: "generic", Status: &disabled})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, updated.StatusCode())

	require.Equal(t, http.StatusAccepted, service.Send(murphytest.Generic("disk-full", "firing")))

	service.StaysWithoutIncidents()
}

func TestAnIncidentPagesItsFirstLevelUntilAcknowledged(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	ana := m.User(murphytest.WithWebhook())
	primary := m.Schedule(team, ana)
	service := m.Service(murphytest.OwnedBy(team), murphytest.Pages(primary.Target()))
	incident := service.Fire("disk-full")
	incident.WaitFor(murphytest.Paged(ana))

	acknowledged := incident.Acknowledge()

	assert.Equal(t, client.IncidentStatusAcknowledged, acknowledged.Status)
	require.NotNil(t, acknowledged.Escalation)
	assert.Equal(t, int32(1), acknowledged.Escalation.Level)
	assert.Equal(t, service.PolicyVersion, acknowledged.Escalation.Policy.Version)
	again := incident.Acknowledge()
	assert.Equal(t, acknowledged.UpdatedAt, again.UpdatedAt, "acknowledging again changes nothing")
	assert.Equal(t, client.IncidentStatusResolved, incident.Resolve().Status)
}
