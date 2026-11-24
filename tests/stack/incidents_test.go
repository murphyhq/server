//go:build stack

package stack

import (
	"net/http"
	"sync"
	"testing"
	"time"

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

func TestTheNextLevelIsPagedWhenTheDelayEnds(t *testing.T) {
	m := murphytest.New(t)
	ana, bo := m.User(murphytest.WithWebhook()), m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Levels(murphytest.Within(time.Second, ana.Target()), murphytest.Within(time.Hour, bo.Target())))

	incident := service.Fire("disk-full")

	incident.WaitFor(
		murphytest.Entries(client.TimelineEntryTypeEscalated, client.TimelineEntryTypeEscalated),
		murphytest.Paged(ana, bo),
	)
	bo.Webhook().WaitForWebhooks(1)
}

func TestAcknowledgingStopsEscalation(t *testing.T) {
	m := murphytest.New(t)
	ana, bo := m.User(murphytest.WithWebhook()), m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Levels(murphytest.Within(2*time.Second, ana.Target()), murphytest.Within(time.Hour, bo.Target())))
	incident := service.Fire("disk-full")
	incident.WaitFor(murphytest.Paged(ana))

	incident.Acknowledge()

	bo.Webhook().ReceivesNothing()
}

func TestEscalationRepeatsThenIsExhausted(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Levels(murphytest.Within(time.Second, ana.Target())), murphytest.Repeats(1))

	incident := service.Fire("disk-full")

	incident.WaitFor(
		murphytest.Entries(client.TimelineEntryTypeEscalated, client.TimelineEntryTypeEscalated, client.TimelineEntryTypeEscalationExhausted),
		murphytest.Paged(ana, ana),
	)
	ana.Webhook().WaitForWebhooks(2)
}

func TestALevelPagesEachPersonItReachesOnce(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	ana, bo := m.User(murphytest.WithWebhook()), m.User(murphytest.WithWebhook())
	primary := m.Schedule(team, ana)
	service := m.Service(murphytest.OwnedBy(team), murphytest.Pages(primary.Target(), bo.Target(), ana.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.Paged(ana, bo))
	ana.Webhook().WaitForWebhooks(1)
}

func TestDeactivatedPeopleAreNotPaged(t *testing.T) {
	m := murphytest.New(t)
	cy, bo := m.User(murphytest.Deactivated(), murphytest.WithWebhook()), m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Pages(cy.Target(), bo.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.Paged(bo), murphytest.Notified(bo, client.Webhook))
	cy.Webhook().ReceivesNothing()
}

func TestAnOverridePagesWhoeverCoversTheSchedule(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	ana, bo := m.User(murphytest.WithWebhook()), m.User(murphytest.WithWebhook())
	primary := m.Schedule(team, ana)
	m.Override(primary, bo, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	service := m.Service(murphytest.OwnedBy(team), murphytest.Pages(primary.Target()))

	incident := service.Fire("disk-full")

	incident.WaitFor(murphytest.PagedVia(bo, primary.Target()))
	ana.Webhook().ReceivesNothing()
}

func TestAnIncidentFollowsThePolicyVersionItOpenedWith(t *testing.T) {
	m := murphytest.New(t)
	ana, bo, cy := m.User(murphytest.WithWebhook()), m.User(murphytest.WithWebhook()), m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Levels(murphytest.Within(2*time.Second, ana.Target()), murphytest.Within(time.Hour, bo.Target())))
	incident := service.Fire("disk-full")

	service.ChangePolicy(murphytest.Within(2*time.Second, ana.Target()), murphytest.Within(time.Hour, cy.Target()))

	incident.WaitFor(murphytest.Paged(ana, bo))
	cy.Webhook().ReceivesNothing()
}

func TestIncidentsFilterByStatusAndServiceAndPaginate(t *testing.T) {
	m := murphytest.New(t)
	service, other := m.Service(), m.Service()
	service.Fire("disk-full")
	service.Fire("cpu-high")
	service.Fire("memory-low").Resolve()
	other.Fire("disk-full")
	triggered := []client.IncidentStatus{client.IncidentStatusTriggered}
	services := []string{service.ID}
	limit := 1

	first, err := m.API.ListIncidentsWithResponse(t.Context(), &client.ListIncidentsParams{Status: &triggered, ServiceId: &services, Limit: &limit})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, first.StatusCode())
	second, err := m.API.ListIncidentsWithResponse(t.Context(), &client.ListIncidentsParams{Status: &triggered, ServiceId: &services, Limit: &limit, Cursor: &first.JSON200.NextCursor})
	require.NoError(t, err)

	assert.Equal(t, "cpu-high", first.JSON200.Items[0].DedupKey, "newest first")
	assert.Equal(t, "disk-full", second.JSON200.Items[0].DedupKey)
	assert.Empty(t, second.JSON200.NextCursor)
}

func TestAnIncidentNamesWhatItPointsAtAndWhoActed(t *testing.T) {
	m := murphytest.New(t)
	ana := m.Member(murphytest.Ana)
	bo := m.User(murphytest.WithWebhook())
	service := m.Service(murphytest.Pages(bo.Target()))
	incident := service.Fire("disk-full")
	incident.WaitFor(murphytest.PagedVia(bo, bo.Target()))

	incident.Acknowledge()
	incident.Resolve()

	got := incident.WaitFor(
		murphytest.ActedBy(client.TimelineEntryTypeAcknowledged, ana.User),
		murphytest.ActedBy(client.TimelineEntryTypeResolved, ana.User),
	)
	assert.Equal(t, &service.Name, got.Service.Name)
	require.NotNil(t, got.Escalation)
	assert.Equal(t, &service.PolicyName, got.Escalation.Policy.Name)
}

func TestIncidentsFilterByTeamPersonPagedUrgencyAndTime(t *testing.T) {
	m := murphytest.New(t)
	team := m.Team()
	bo := m.User(murphytest.WithWebhook())
	owned := m.Service(murphytest.OwnedBy(team), murphytest.Pages(bo.Target()))
	low := m.Service(murphytest.Urgency(client.Low))
	mine := owned.Fire("disk-full")
	mine.WaitFor(murphytest.Paged(bo))
	quiet := low.Fire("disk-full")
	both := []string{owned.ID, low.ID}
	teams, paged, urgencies := []string{team.ID}, []string{bo.ID}, []client.Urgency{client.Low}
	later := time.Now().Add(time.Hour)

	byTeam := m.IncidentsMatching(client.ListIncidentsParams{TeamId: &teams})
	byPerson := m.IncidentsMatching(client.ListIncidentsParams{PagedUserId: &paged})
	byUrgency := m.IncidentsMatching(client.ListIncidentsParams{ServiceId: &both, Urgency: &urgencies})
	afterNow := m.IncidentsMatching(client.ListIncidentsParams{ServiceId: &both, CreatedAfter: &later})
	beforeNow := m.IncidentsMatching(client.ListIncidentsParams{ServiceId: &both, CreatedBefore: &later})

	assert.Equal(t, []string{mine.ID}, byTeam)
	assert.Equal(t, []string{mine.ID}, byPerson)
	assert.Equal(t, []string{quiet.ID}, byUrgency)
	assert.Empty(t, afterNow)
	assert.Equal(t, []string{quiet.ID, mine.ID}, beforeNow, "newest first")
}

func TestATeamWithoutPoliciesOwnsNoIncidents(t *testing.T) {
	m := murphytest.New(t)
	teams := []string{m.Team().ID}

	got := m.IncidentsMatching(client.ListIncidentsParams{TeamId: &teams})

	assert.Empty(t, got)
}
