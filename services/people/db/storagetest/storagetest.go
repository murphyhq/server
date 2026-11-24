package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/people"
)

const maxID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

type Storage interface {
	people.Repository
	people.AuthRepository
}

func Run(t *testing.T, open func(t *testing.T) Storage) {
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

	t.Run("hands a sign-in out once", func(t *testing.T) {
		repo := open(t)
		now := time.Now().UTC()
		signIn := newSignIn("state", now.Add(time.Minute))
		signIn.ReturnTo = "http://localhost:8090/"
		signIn.CodeChallenge = "challenge"
		require.NoError(t, repo.StartSignIn(t.Context(), signIn, now))

		first, err := repo.TakeSignIn(t.Context(), []byte("state"), now)
		require.NoError(t, err)
		_, again := repo.TakeSignIn(t.Context(), []byte("state"), now)

		assert.Equal(t, "verifier", first.GetVerifier())
		assert.Equal(t, "http://localhost:8090/", first.GetReturnTo())
		assert.Equal(t, "challenge", first.GetCodeChallenge())
		assert.ErrorIs(t, again, murphy.ErrNotFound)
	})

	t.Run("never hands out an expired sign-in", func(t *testing.T) {
		repo := open(t)
		now := time.Now().UTC()
		require.NoError(t, repo.StartSignIn(t.Context(), newSignIn("state", now.Add(time.Minute)), now))

		_, err := repo.TakeSignIn(t.Context(), []byte("state"), now.Add(2*time.Minute))

		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("hands a one-time code out once, with its challenge", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), user))
		require.NoError(t, repo.CreateOneTimeCode(t.Context(), newOneTimeCode(user, "code", "challenge", now.Add(time.Minute))))

		first, err := repo.TakeOneTimeCode(t.Context(), []byte("code"), now)
		require.NoError(t, err)
		_, again := repo.TakeOneTimeCode(t.Context(), []byte("code"), now)

		assert.Equal(t, user.GetId(), first.GetUserId())
		assert.Equal(t, "challenge", first.GetCodeChallenge())
		assert.Equal(t, murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_APP_SIGN_IN, first.GetPurpose())
		assert.ErrorIs(t, again, murphy.ErrNotFound)
	})

	t.Run("never hands out an expired one-time code", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), user))
		require.NoError(t, repo.CreateOneTimeCode(t.Context(), newOneTimeCode(user, "code", "", now.Add(time.Minute))))

		_, err := repo.TakeOneTimeCode(t.Context(), []byte("code"), now.Add(2*time.Minute))

		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("finds a person by their identity at the provider", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")
		require.NoError(t, repo.CreateUserWithIdentity(t.Context(), user, newIdentity(user, "ana-at-the-provider")))

		got, err := repo.GetIdentity(t.Context(), "https://id.example.com", "ana-at-the-provider")

		require.NoError(t, err)
		assert.Equal(t, user.GetId(), got.GetUserId())
	})

	t.Run("creates a user only together with their identity", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		require.NoError(t, repo.CreateUserWithIdentity(t.Context(), ana, newIdentity(ana, "taken")))

		err := repo.CreateUserWithIdentity(t.Context(), bo, newIdentity(bo, "taken"))
		_, lookup := repo.GetUserByEmail(t.Context(), "bo@example.com")

		assert.ErrorIs(t, err, murphy.ErrConflict)
		assert.ErrorIs(t, lookup, murphy.ErrNotFound, "the user is not created without the identity")
	})

	t.Run("a session works until it is revoked", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), user))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(user, "token", now.Add(time.Hour))))

		active, role, err := repo.GetActiveSession(t.Context(), []byte("token"), now)
		require.NoError(t, err)
		require.NoError(t, repo.RevokeSession(t.Context(), []byte("token"), now))
		_, _, revoked := repo.GetActiveSession(t.Context(), []byte("token"), now)

		assert.Equal(t, user.GetId(), active.GetUserId())
		assert.Equal(t, murphy.UserRole_USER_ROLE_MEMBER, role, "the session says what the user may do")
		assert.ErrorIs(t, revoked, murphy.ErrNotFound)
	})

	t.Run("a session stops working when it expires", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), user))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(user, "token", now.Add(time.Hour))))

		_, _, err := repo.GetActiveSession(t.Context(), []byte("token"), now.Add(2*time.Hour))

		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("a session stops working when its user is deactivated", func(t *testing.T) {
		repo := open(t)
		user := newUser("ana@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), user))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(user, "token", now.Add(time.Hour))))
		user.Status = murphy.UserStatus_USER_STATUS_DEACTIVATED
		require.NoError(t, repo.UpdateUser(t.Context(), user))

		_, _, err := repo.GetActiveSession(t.Context(), []byte("token"), now)

		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("lists only a user's active sessions, newest first", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), ana))
		require.NoError(t, repo.CreateUser(t.Context(), bo))
		older, newer := newSession(ana, "older", now.Add(time.Hour)), newSession(ana, "newer", now.Add(time.Hour))
		require.NoError(t, repo.CreateSession(t.Context(), older))
		require.NoError(t, repo.CreateSession(t.Context(), newer))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(ana, "expired", now.Add(-time.Minute))))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(bo, "bos", now.Add(time.Hour))))

		got, err := repo.ListActiveSessions(t.Context(), ana.GetId(), maxID, 10, now)

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, []string{newer.GetId(), older.GetId()}, []string{got[0].GetId(), got[1].GetId()})
		assert.Equal(t, murphy.SessionClient_SESSION_CLIENT_TOKEN, got[0].GetClient())
	})

	t.Run("a session is ended only by its own user", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), ana))
		require.NoError(t, repo.CreateUser(t.Context(), bo))
		anas := newSession(ana, "anas", now.Add(time.Hour))
		require.NoError(t, repo.CreateSession(t.Context(), anas))

		byBo := repo.RevokeSessionOfUser(t.Context(), bo.GetId(), anas.GetId(), now)
		_, _, stillActive := repo.GetActiveSession(t.Context(), []byte("anas"), now)
		byAna := repo.RevokeSessionOfUser(t.Context(), ana.GetId(), anas.GetId(), now)
		_, _, ended := repo.GetActiveSession(t.Context(), []byte("anas"), now)

		assert.ErrorIs(t, byBo, murphy.ErrNotFound)
		assert.NoError(t, stillActive)
		assert.NoError(t, byAna)
		assert.ErrorIs(t, ended, murphy.ErrNotFound)
	})

	t.Run("ending all of a user's sessions also discards their unused codes", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		now := time.Now().UTC()
		require.NoError(t, repo.CreateUser(t.Context(), ana))
		require.NoError(t, repo.CreateUser(t.Context(), bo))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(ana, "phone", now.Add(time.Hour))))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(ana, "laptop", now.Add(time.Hour))))
		require.NoError(t, repo.CreateSession(t.Context(), newSession(bo, "bos", now.Add(time.Hour))))
		require.NoError(t, repo.CreateOneTimeCode(t.Context(), newOneTimeCode(ana, "connect", "", now.Add(time.Minute))))

		require.NoError(t, repo.EndSessionsOfUser(t.Context(), ana.GetId(), now))
		anas, err := repo.ListActiveSessions(t.Context(), ana.GetId(), maxID, 10, now)
		require.NoError(t, err)
		_, code := repo.TakeOneTimeCode(t.Context(), []byte("connect"), now)
		_, _, bos := repo.GetActiveSession(t.Context(), []byte("bos"), now)

		assert.Empty(t, anas)
		assert.ErrorIs(t, code, murphy.ErrNotFound)
		assert.NoError(t, bos, "other people's sessions are untouched")
	})

	t.Run("the first admin is claimed once", func(t *testing.T) {
		repo := open(t)
		ana, bo := newUser("ana@example.com"), newUser("bo@example.com")
		require.NoError(t, repo.CreateUser(t.Context(), ana))
		require.NoError(t, repo.CreateUser(t.Context(), bo))

		first, err := repo.ClaimFirstAdmin(t.Context(), ana.GetId(), time.Now().UTC())
		require.NoError(t, err)
		second, err := repo.ClaimFirstAdmin(t.Context(), bo.GetId(), time.Now().UTC())
		require.NoError(t, err)
		admin, err := repo.GetUser(t.Context(), ana.GetId())
		require.NoError(t, err)

		assert.True(t, first)
		assert.False(t, second)
		assert.Equal(t, murphy.UserRole_USER_ROLE_ADMIN, admin.GetRole())
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

	t.Run("lists the contact methods of many users in one call", func(t *testing.T) {
		repo := open(t)
		ana, bo, cy := newUser("ana@example.com"), newUser("bo@example.com"), newUser("cy@example.com")
		for _, user := range []*murphy.User{ana, bo, cy} {
			require.NoError(t, repo.CreateUser(t.Context(), user))
		}
		anaHook, anaEmail, boHook, cyHook := newContactMethod(ana, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK), newContactMethod(ana, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL), newContactMethod(bo, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK), newContactMethod(cy, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK)
		for _, method := range []*murphy.ContactMethod{anaHook, anaEmail, boHook, cyHook} {
			require.NoError(t, repo.CreateContactMethod(t.Context(), method))
		}

		methods, err := repo.ListContactMethodsOfUsers(t.Context(), []string{ana.GetId(), bo.GetId(), murphy.NewID()})

		require.NoError(t, err)
		ids := make([]string, len(methods))
		for i, method := range methods {
			ids[i] = method.GetId()
		}
		assert.ElementsMatch(t, []string{anaHook.GetId(), anaEmail.GetId(), boHook.GetId()}, ids)
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
		Status: murphy.UserStatus_USER_STATUS_ACTIVE, Role: murphy.UserRole_USER_ROLE_MEMBER, CreatedAt: now, UpdatedAt: now,
	}
}

func newSignIn(state string, expiresAt time.Time) *murphy.SignIn {
	now := murphy.Now()
	return &murphy.SignIn{
		Id: murphy.NewID(), StateHash: []byte(state), Nonce: "nonce", Verifier: "verifier",
		ExpiresAt: timestamppb.New(expiresAt), CreatedAt: now, UpdatedAt: now,
	}
}

func newIdentity(user *murphy.User, subject string) *murphy.Identity {
	now := murphy.Now()
	return &murphy.Identity{Id: murphy.NewID(), UserId: user.GetId(), Issuer: "https://id.example.com", Subject: subject, CreatedAt: now, UpdatedAt: now}
}

func newSession(user *murphy.User, token string, expiresAt time.Time) *murphy.Session {
	now := murphy.Now()
	return &murphy.Session{
		Id: murphy.NewID(), UserId: user.GetId(), TokenHash: []byte(token), Client: murphy.SessionClient_SESSION_CLIENT_TOKEN,
		ExpiresAt: timestamppb.New(expiresAt), CreatedAt: now, UpdatedAt: now,
	}
}

func newContactMethod(user *murphy.User, kind murphy.ContactMethodType) *murphy.ContactMethod {
	now := murphy.Now()
	return &murphy.ContactMethod{Id: murphy.NewID(), UserId: user.GetId(), Type: kind, Address: "https://example.com/" + murphy.NewID(), CreatedAt: now, UpdatedAt: now}
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

func newOneTimeCode(user *murphy.User, code, challenge string, expiresAt time.Time) *murphy.OneTimeCode {
	now := murphy.Now()
	return &murphy.OneTimeCode{
		Id: murphy.NewID(), CodeHash: []byte(code), UserId: user.GetId(), Purpose: murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_APP_SIGN_IN,
		CodeChallenge: challenge, ExpiresAt: timestamppb.New(expiresAt), CreatedAt: now, UpdatedAt: now,
	}
}
