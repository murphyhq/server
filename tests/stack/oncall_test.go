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

var rotationStart = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

func references(ids ...string) []client.Reference {
	out := make([]client.Reference, len(ids))
	for i, id := range ids {
		out[i] = client.Reference{Id: id}
	}
	return out
}

func dailyRotation(m *murphytest.Murphy, teams []string, participants ...string) client.ScheduleInput {
	teamRefs := references(teams...)
	return client.ScheduleInput{
		Name:   m.Name("schedule"),
		Teams:  &teamRefs,
		Layers: []client.Layer{{TurnLengthSeconds: 24 * 60 * 60, StartsAt: rotationStart, Participants: references(participants...)}},
	}
}

func createSchedule(t *testing.T, m *murphytest.Murphy, input client.ScheduleInput) string {
	t.Helper()
	created, err := m.API.CreateScheduleWithResponse(t.Context(), nil, input)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	return created.JSON201.Id
}

func TestSchedulesAnswerWhoIsOnCall(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	ana, bo, cy := m.User(), m.User(), m.User()
	schedule := createSchedule(t, m, dailyRotation(m, nil, ana.ID, bo.ID))

	second := rotationStart.Add(25 * time.Hour)
	onCall, err := m.API.GetOnCallWithResponse(ctx, schedule, &client.GetOnCallParams{At: &second})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, onCall.StatusCode())
	assert.Equal(t, bo.ID, onCall.JSON200.User.Id, "the second day is Bo's turn")

	override, err := m.API.CreateOverrideWithResponse(ctx, schedule, nil, client.OverrideInput{
		User: client.Reference{Id: cy.ID}, StartsAt: second.Add(-time.Hour), EndsAt: second.Add(time.Hour),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, override.StatusCode())
	onCall, err = m.API.GetOnCallWithResponse(ctx, schedule, &client.GetOnCallParams{At: &second})
	require.NoError(t, err)
	assert.Equal(t, cy.ID, onCall.JSON200.User.Id, "an override wins")
}

func TestSchedulesRefuseUnknownPeopleAndTeams(t *testing.T) {
	m := murphytest.New(t)
	ana := m.User()

	for name, input := range map[string]client.ScheduleInput{
		"participant": dailyRotation(m, nil, unknown),
		"team":        dailyRotation(m, []string{unknown}, ana.ID),
	} {
		created, err := m.API.CreateScheduleWithResponse(t.Context(), nil, input)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, created.StatusCode(), name)
	}
}

func TestSchedulesFilterByTeam(t *testing.T) {
	m := murphytest.New(t)
	platform, payments := m.Team(), m.Team()
	ana := m.User()
	shared := createSchedule(t, m, dailyRotation(m, []string{platform.ID, payments.ID}, ana.ID))
	createSchedule(t, m, dailyRotation(m, []string{platform.ID}, ana.ID))

	page, err := m.API.ListSchedulesWithResponse(t.Context(), &client.ListSchedulesParams{TeamId: &payments.ID})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, page.StatusCode())
	require.Len(t, page.JSON200.Items, 1)
	assert.Equal(t, shared, page.JSON200.Items[0].Id)
	assert.ElementsMatch(t, []client.Reference{platform.Reference(), payments.Reference()}, page.JSON200.Items[0].Teams)
}

func TestCreatesAreIdempotent(t *testing.T) {
	m := murphytest.New(t)
	ctx := t.Context()
	key := &client.CreateServiceParams{IdempotencyKey: new(uuid.NewV4().String())}
	policy := m.Policy()
	input := client.ServiceInput{Name: m.Name("service"), EscalationPolicy: client.Reference{Id: policy.ID}}

	first, err := m.API.CreateServiceWithResponse(ctx, key, input)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, first.StatusCode())
	retry, err := m.API.CreateServiceWithResponse(ctx, key, input)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, retry.StatusCode())
	assert.Equal(t, first.JSON201.Id, retry.JSON201.Id, "the retry answers with the original response")

	input.Name = m.Name("service")
	different, err := m.API.CreateServiceWithResponse(ctx, key, input)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnprocessableEntity, different.StatusCode(), "the same key with another request is refused")
}

func TestWhoIsOnCallAcrossSchedulesInOneRequest(t *testing.T) {
	m := murphytest.New(t)
	ana, bo := m.User(), m.User()
	rotation := m.Schedule(m.Team(), ana, bo)
	people := []string{ana.ID, bo.ID}

	got, err := m.API.ListOnCallWithResponse(t.Context(), &client.ListOnCallParams{UserId: &people})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode())
	require.Len(t, got.JSON200.Items, 1, "Bo takes turns there but isn't on call now")
	assert.Equal(t, client.Reference{Id: rotation.ID, Name: &rotation.Name}, got.JSON200.Items[0].Schedule)
	assert.Equal(t, new(ana.Reference()), got.JSON200.Items[0].User)
}
