package storagetest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/people"
)

const maxID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

func Run(t *testing.T, open func(t *testing.T) people.Repository) {
	t.Run("stores and reads back a user", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")

		require.NoError(t, repo.CreateUser(t.Context(), user))
		got, err := repo.GetUser(t.Context(), user.GetId())

		require.NoError(t, err)
		assert.Equal(t, user.GetEmail(), got.GetEmail())
		assert.Equal(t, user.GetTimeZone(), got.GetTimeZone())
		assert.Equal(t, user.GetStatus(), got.GetStatus())
	})

	t.Run("refuses a second user with the same email", func(t *testing.T) {
		repo := open(t)
		require.NoError(t, repo.CreateUser(t.Context(), newUser("ana@example.com")))

		assert.ErrorIs(t, repo.CreateUser(t.Context(), newUser("ana@example.com")), murphy.ErrConflict)
	})

	t.Run("refuses a second team with the same name", func(t *testing.T) {
		repo := open(t)
		require.NoError(t, repo.CreateTeam(t.Context(), newTeam("platform")))

		assert.ErrorIs(t, repo.CreateTeam(t.Context(), newTeam("platform")), murphy.ErrConflict)
	})

	t.Run("lists teams newest first from a cursor", func(t *testing.T) {
		repo := open(t)
		first, second := newTeam("platform"), newTeam("payments")
		require.NoError(t, repo.CreateTeam(t.Context(), first))
		require.NoError(t, repo.CreateTeam(t.Context(), second))

		all, err := repo.ListTeams(t.Context(), maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.GetId(), first.GetId()}, teamIDs(all))

		page, err := repo.ListTeams(t.Context(), second.GetId(), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.GetId()}, teamIDs(page))
	})

	t.Run("setting a membership again changes the role", func(t *testing.T) {
		repo := open(t)
		team, user := newTeam("platform"), newUser("ana@example.com")
		require.NoError(t, repo.CreateTeam(t.Context(), team))
		require.NoError(t, repo.CreateUser(t.Context(), user))

		require.NoError(t, repo.SetMembership(t.Context(), newMembership(team, user, murphy.TeamRole_TEAM_ROLE_RESPONDER)))
		require.NoError(t, repo.SetMembership(t.Context(), newMembership(team, user, murphy.TeamRole_TEAM_ROLE_MANAGER)))

		members, err := repo.ListMemberships(t.Context(), team.GetId())
		require.NoError(t, err)
		require.Len(t, members, 1)
		assert.Equal(t, murphy.TeamRole_TEAM_ROLE_MANAGER, members[0].GetRole())
	})

	t.Run("deleting a team or a user removes their memberships", func(t *testing.T) {
		repo := open(t)
		platform, payments := newTeam("platform"), newTeam("payments")
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		for _, team := range []*murphy.Team{platform, payments} {
			require.NoError(t, repo.CreateTeam(t.Context(), team))
		}
		for _, user := range []*murphy.User{ana, bo} {
			require.NoError(t, repo.CreateUser(t.Context(), user))
			require.NoError(t, repo.SetMembership(t.Context(), newMembership(platform, user, murphy.TeamRole_TEAM_ROLE_RESPONDER)))
		}
		require.NoError(t, repo.SetMembership(t.Context(), newMembership(payments, ana, murphy.TeamRole_TEAM_ROLE_OBSERVER)))

		require.NoError(t, repo.DeleteUser(t.Context(), bo.GetId()))
		require.NoError(t, repo.DeleteTeam(t.Context(), payments.GetId()))

		members, err := repo.ListMemberships(t.Context(), platform.GetId())
		require.NoError(t, err)
		require.Len(t, members, 1)
		assert.Equal(t, ana.GetId(), members[0].GetUserId())
		_, err = repo.GetMembership(t.Context(), payments.GetId(), ana.GetId())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("finds which of many users and teams exist in one call", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		team := newTeam("platform")
		require.NoError(t, repo.CreateUser(t.Context(), ana))
		require.NoError(t, repo.CreateUser(t.Context(), bo))
		require.NoError(t, repo.CreateTeam(t.Context(), team))

		users, err := repo.ExistingUsers(t.Context(), []string{ana.GetId(), murphy.NewID(), bo.GetId()})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{ana.GetId(), bo.GetId()}, users)

		teams, err := repo.ExistingTeams(t.Context(), []string{murphy.NewID(), team.GetId()})
		require.NoError(t, err)
		assert.Equal(t, []string{team.GetId()}, teams)
	})

	t.Run("finds which of many users are active in one call", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		bo.Status = murphy.UserStatus_USER_STATUS_DEACTIVATED
		require.NoError(t, repo.CreateUser(t.Context(), ana))
		require.NoError(t, repo.CreateUser(t.Context(), bo))

		active, err := repo.ActiveUsers(t.Context(), []string{ana.GetId(), bo.GetId(), murphy.NewID()})

		require.NoError(t, err)
		assert.Equal(t, []string{ana.GetId()}, active)
	})

	t.Run("reports missing users, teams and memberships", func(t *testing.T) {
		repo := open(t)
		_, err := repo.GetUser(t.Context(), murphy.NewID())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		_, err = repo.GetTeam(t.Context(), murphy.NewID())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		assert.ErrorIs(t, repo.UpdateTeam(t.Context(), newTeam("ghost")), murphy.ErrNotFound)
		assert.ErrorIs(t, repo.DeleteTeam(t.Context(), murphy.NewID()), murphy.ErrNotFound)
		assert.ErrorIs(t, repo.DeleteMembership(t.Context(), murphy.NewID(), murphy.NewID()), murphy.ErrNotFound)
	})
}

func newUser(email string) *murphy.User {
	now := murphy.Now()
	return &murphy.User{
		Id: murphy.NewID(), Name: email, Email: email, TimeZone: "Europe/Malta",
		Status: murphy.UserStatus_USER_STATUS_ACTIVE, CreatedAt: now, UpdatedAt: now,
	}
}

func newTeam(name string) *murphy.Team {
	now := murphy.Now()
	return &murphy.Team{Id: murphy.NewID(), Name: name, CreatedAt: now, UpdatedAt: now}
}

func newMembership(team *murphy.Team, user *murphy.User, role murphy.TeamRole) *murphy.TeamMembership {
	now := murphy.Now()
	return &murphy.TeamMembership{Id: murphy.NewID(), TeamId: team.GetId(), UserId: user.GetId(), Role: role, CreatedAt: now, UpdatedAt: now}
}

func teamIDs(teams []*murphy.Team) []string {
	ids := make([]string, len(teams))
	for i, team := range teams {
		ids[i] = team.GetId()
	}
	return ids
}
