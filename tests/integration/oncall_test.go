//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/testenv"
)

var rotationStart = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

func TestSchedulesAnswerWhoIsOnCall(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		ctx := t.Context()
		ana, bo, cy := createUser(t, api, "ana@example.com"), createUser(t, api, "bo@example.com"), createUser(t, api, "cy@example.com")
		schedule := createSchedule(t, api, "primary", nil, ana, bo)

		second := rotationStart.Add(25 * time.Hour)
		onCall, err := api.GetOnCallWithResponse(ctx, schedule, &client.GetOnCallParams{At: &second})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, onCall.StatusCode())
		assert.Equal(t, bo, onCall.JSON200.User.Id)

		override, err := api.CreateOverrideWithResponse(ctx, schedule, nil, client.OverrideInput{
			User: client.Reference{Id: cy}, StartsAt: second.Add(-time.Hour), EndsAt: second.Add(time.Hour),
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, override.StatusCode())
		onCall, err = api.GetOnCallWithResponse(ctx, schedule, &client.GetOnCallParams{At: &second})
		require.NoError(t, err)
		assert.Equal(t, cy, onCall.JSON200.User.Id)
	})
}

func TestSchedulesRefuseUnknownPeopleAndTeams(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		unknown := "0195f3a0-0000-7000-8000-000000000000"
		ana := createUser(t, api, "ana@example.com")

		for name, input := range map[string]client.ScheduleInput{
			"participant": scheduleInput("primary", nil, unknown),
			"team":        scheduleInput("primary", []string{unknown}, ana),
		} {
			created, err := api.CreateScheduleWithResponse(t.Context(), nil, input)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, created.StatusCode(), name)
		}
	})
}

func TestSchedulesFilterByTeam(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		platform, payments := createTeam(t, api, "platform"), createTeam(t, api, "payments")
		ana := createUser(t, api, "ana@example.com")
		shared := createSchedule(t, api, "commanders", []string{platform, payments}, ana)
		createSchedule(t, api, "platform primary", []string{platform}, ana)

		page, err := api.ListSchedulesWithResponse(t.Context(), &client.ListSchedulesParams{TeamId: &payments})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, page.StatusCode())
		require.Len(t, page.JSON200.Items, 1)
		assert.Equal(t, shared, page.JSON200.Items[0].Id)
		assert.ElementsMatch(t, references(platform, payments), page.JSON200.Items[0].Teams)
	})
}

func scheduleInput(name string, teams []string, participants ...string) client.ScheduleInput {
	teamRefs := references(teams...)
	return client.ScheduleInput{
		Name:   name,
		Teams:  &teamRefs,
		Layers: []client.Layer{{TurnLengthSeconds: 24 * 60 * 60, StartsAt: rotationStart, Participants: references(participants...)}},
	}
}

func references(ids ...string) []client.Reference {
	out := make([]client.Reference, len(ids))
	for i, id := range ids {
		out[i] = client.Reference{Id: id}
	}
	return out
}

func createSchedule(t *testing.T, api *client.ClientWithResponses, name string, teams []string, participants ...string) string {
	t.Helper()
	created, err := api.CreateScheduleWithResponse(t.Context(), nil, scheduleInput(name, teams, participants...))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	return created.JSON201.Id
}
