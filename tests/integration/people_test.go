//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/testenv"
)

func TestTeamsAndTheirMembers(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		api, _ := testenv.API(t, engine)
		ctx := t.Context()
		team := createTeam(t, api, "platform")
		ana := createUser(t, api, "ana@example.com")

		added, err := api.SetTeamMemberWithResponse(ctx, team, ana, client.TeamMemberInput{Role: client.Responder})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, added.StatusCode())
		promoted, err := api.SetTeamMemberWithResponse(ctx, team, ana, client.TeamMemberInput{Role: client.Manager})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, promoted.StatusCode())

		members, err := api.ListTeamMembersWithResponse(ctx, team)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, members.StatusCode())
		require.Len(t, members.JSON200.Items, 1)
		assert.Equal(t, client.Manager, members.JSON200.Items[0].Role)

		stranger, err := api.SetTeamMemberWithResponse(ctx, team, "0195f3a0-0000-7000-8000-000000000000", client.TeamMemberInput{Role: client.Observer})
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, stranger.StatusCode())

		removed, err := api.RemoveTeamMemberWithResponse(ctx, team, ana)
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, removed.StatusCode())
		members, err = api.ListTeamMembersWithResponse(ctx, team)
		require.NoError(t, err)
		assert.Empty(t, members.JSON200.Items)
	})
}

func createTeam(t *testing.T, api *client.ClientWithResponses, name string) string {
	t.Helper()
	created, err := api.CreateTeamWithResponse(t.Context(), nil, client.TeamInput{Name: name})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	return created.JSON201.Id
}

func createUser(t *testing.T, api *client.ClientWithResponses, email string) string {
	t.Helper()
	created, err := api.CreateUserWithResponse(t.Context(), nil, client.UserInput{Name: email, Email: email})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	return created.JSON201.Id
}
