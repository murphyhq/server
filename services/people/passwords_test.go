package people_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/people"
)

const (
	address       = "203.0.113.7"
	goodPassword  = "correct horse battery staple"
	otherPassword = "not the right password at all"
)

type passwords struct {
	auth    *people.Auth
	repo    *MockAuthRepository
	limiter *MockLimiter
	bo      *murphy.User
	users   map[string]*murphy.User
	hashes  map[string]string
	codes   map[string]*murphy.OneTimeCode
	hits    map[string]int64
	resets  []string
	ended   []string
	created []*murphy.User
	admin   string
}

func withPasswords(t *testing.T) *passwords {
	t.Helper()
	repo, limiter := NewMockAuthRepository(t), NewMockLimiter(t)
	allowed := settings()
	allowed.PasswordsAllowed = true
	allowed.InitialAdminPassword = "the configured admin password"
	p := &passwords{
		auth: people.NewAuth(repo, nil, limiter, allowed), repo: repo, limiter: limiter,
		bo:    &murphy.User{Id: murphy.NewID(), Email: "bo@example.com", Status: murphy.UserStatus_USER_STATUS_ACTIVE},
		users: map[string]*murphy.User{}, hashes: map[string]string{}, codes: map[string]*murphy.OneTimeCode{}, hits: map[string]int64{},
	}
	p.users[p.bo.GetEmail()] = p.bo
	repo.EXPECT().GetUserByEmail(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, email string) (*murphy.User, error) {
		if user, ok := p.users[email]; ok {
			return user, nil
		}
		return nil, &murphy.NotFoundError{Entity: "user"}
	}).Maybe()
	repo.EXPECT().GetUser(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, id string) (*murphy.User, error) {
		for _, user := range p.users {
			if user.GetId() == id {
				return user, nil
			}
		}
		return nil, &murphy.NotFoundError{Entity: "user"}
	}).Maybe()
	repo.EXPECT().CreateUser(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, user *murphy.User) error {
		p.created = append(p.created, user)
		p.users[user.GetEmail()] = user
		return nil
	}).Maybe()
	repo.EXPECT().ClaimFirstAdmin(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string, _ time.Time) (bool, error) {
		if p.admin != "" {
			return false, nil
		}
		p.admin = userID
		for _, user := range p.users {
			if user.GetId() == userID {
				user.Role = murphy.UserRole_USER_ROLE_ADMIN
			}
		}
		return true, nil
	}).Maybe()
	repo.EXPECT().GetPasswordHash(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string) (string, error) {
		if stored, ok := p.hashes[userID]; ok {
			return stored, nil
		}
		return "", &murphy.NotFoundError{Entity: "password"}
	}).Maybe()
	repo.EXPECT().SetPassword(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID, hash string, _ time.Time) error {
		p.hashes[userID] = hash
		return nil
	}).Maybe()
	repo.EXPECT().EndSessionsOfUser(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string, _ time.Time) error {
		p.ended = append(p.ended, userID)
		return nil
	}).Maybe()
	repo.EXPECT().CreateOneTimeCode(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, code *murphy.OneTimeCode) error {
		p.codes[string(code.GetCodeHash())] = code
		return nil
	}).Maybe()
	repo.EXPECT().TakeOneTimeCode(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, codeHash []byte, _ time.Time) (*murphy.OneTimeCode, error) {
		code, ok := p.codes[string(codeHash)]
		if !ok {
			return nil, &murphy.NotFoundError{Entity: "one-time code"}
		}
		delete(p.codes, string(codeHash))
		return code, nil
	}).Maybe()
	repo.EXPECT().CreateSession(mock.Anything, mock.Anything).Return(nil).Maybe()
	limiter.EXPECT().Count(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, key string) (int64, error) {
		return p.hits[key], nil
	}).Maybe()
	limiter.EXPECT().Hit(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, key string, _ time.Duration) (int64, error) {
		p.hits[key]++
		return p.hits[key], nil
	}).Maybe()
	limiter.EXPECT().Reset(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, key string) error {
		p.resets = append(p.resets, key)
		delete(p.hits, key)
		return nil
	}).Maybe()
	return p
}

func (p *passwords) linkCode(t *testing.T, userID string) string {
	t.Helper()
	link, err := p.auth.CreatePasswordLink(t.Context(), userID)
	require.NoError(t, err)
	parsed, err := url.Parse(link.Link)
	require.NoError(t, err)
	return parsed.Query().Get("code")
}

func (p *passwords) withPassword(t *testing.T, password string) {
	t.Helper()
	require.NoError(t, p.auth.SetPassword(t.Context(), p.linkCode(t, p.bo.GetId()), password))
}

func TestTheRightPasswordSignsIn(t *testing.T) {
	p := withPasswords(t)
	p.withPassword(t, goodPassword)

	signedIn, err := p.auth.SignInWithPassword(t.Context(), "bo@example.com", goodPassword, address)

	require.NoError(t, err)
	assert.Equal(t, p.bo.GetId(), signedIn.Session.GetUserId())
	assert.Contains(t, p.resets, "sign-in:person:bo@example.com", "earlier failures are forgotten")
}

func TestOnlyAHashIsStored(t *testing.T) {
	p := withPasswords(t)

	p.withPassword(t, goodPassword)

	assert.True(t, strings.HasPrefix(p.hashes[p.bo.GetId()], "$argon2id$"))
	assert.NotContains(t, p.hashes[p.bo.GetId()], goodPassword)
}

func TestAWrongPasswordIsRefusedAndCountedForThePersonAndTheAddress(t *testing.T) {
	p := withPasswords(t)
	p.withPassword(t, goodPassword)

	_, err := p.auth.SignInWithPassword(t.Context(), "bo@example.com", otherPassword, address)

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Equal(t, map[string]int64{"sign-in:person:bo@example.com": 1, "sign-in:address:" + address: 1}, p.hits)
}

func TestSomeoneUnknownGetsTheSameAnswerAsAWrongPassword(t *testing.T) {
	p := withPasswords(t)

	_, err := p.auth.SignInWithPassword(t.Context(), "nobody@example.com", goodPassword, address)

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Equal(t, int64(1), p.hits["sign-in:person:nobody@example.com"])
}

func TestADeactivatedPersonCantSignInWithTheirPassword(t *testing.T) {
	p := withPasswords(t)
	p.withPassword(t, goodPassword)
	p.bo.Status = murphy.UserStatus_USER_STATUS_DEACTIVATED

	_, err := p.auth.SignInWithPassword(t.Context(), "bo@example.com", goodPassword, address)

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
}

func TestTooManyFailuresForAPersonAreRefusedEvenWithTheRightPassword(t *testing.T) {
	p := withPasswords(t)
	p.withPassword(t, goodPassword)
	p.hits["sign-in:person:bo@example.com"] = 5

	_, err := p.auth.SignInWithPassword(t.Context(), "bo@example.com", goodPassword, address)

	assert.ErrorIs(t, err, murphy.ErrTooManyAttempts)
}

func TestTooManyFailuresFromAnAddressAreRefused(t *testing.T) {
	p := withPasswords(t)
	p.withPassword(t, goodPassword)
	p.hits["sign-in:address:"+address] = 50

	_, err := p.auth.SignInWithPassword(t.Context(), "bo@example.com", goodPassword, address)

	assert.ErrorIs(t, err, murphy.ErrTooManyAttempts)
}

func TestAPasswordOutsideTheRulesLeavesTheLinkUsable(t *testing.T) {
	p := withPasswords(t)
	code := p.linkCode(t, p.bo.GetId())

	short := p.auth.SetPassword(t.Context(), code, "too short")
	fine := p.auth.SetPassword(t.Context(), code, goodPassword)

	assert.ErrorIs(t, short, murphy.ErrInvalid)
	assert.NoError(t, fine)
}

func TestALinkSetsAPasswordOnce(t *testing.T) {
	p := withPasswords(t)
	code := p.linkCode(t, p.bo.GetId())
	require.NoError(t, p.auth.SetPassword(t.Context(), code, goodPassword))

	err := p.auth.SetPassword(t.Context(), code, otherPassword)

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
}

func TestSettingAPasswordEndsThePersonsSessions(t *testing.T) {
	p := withPasswords(t)

	p.withPassword(t, goodPassword)

	assert.Equal(t, []string{p.bo.GetId()}, p.ended)
}

func TestASetPasswordLinkDoesntSignAnyoneIn(t *testing.T) {
	p := withPasswords(t)

	_, err := p.auth.SignInWithCode(t.Context(), p.linkCode(t, p.bo.GetId()), "")

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
}

func TestALinkForSomeoneMissingIsNotFound(t *testing.T) {
	p := withPasswords(t)

	_, err := p.auth.CreatePasswordLink(t.Context(), murphy.NewID())

	assert.ErrorIs(t, err, murphy.ErrNotFound)
}

func TestAnInstallThatDoesntAllowPasswordsRefusesThem(t *testing.T) {
	repo := NewMockAuthRepository(t)
	auth := people.NewAuth(repo, NewMockProvider(t), nil, settings())

	_, signIn := auth.SignInWithPassword(t.Context(), "bo@example.com", goodPassword, address)
	_, link := auth.CreatePasswordLink(t.Context(), murphy.NewID())
	set := auth.SetPassword(t.Context(), "mpl_anything", goodPassword)

	assert.ErrorIs(t, signIn, murphy.ErrForbidden)
	assert.ErrorIs(t, link, murphy.ErrForbidden)
	assert.ErrorIs(t, set, murphy.ErrForbidden)
}

func TestAnInstallWithoutAProviderOnlySignsInWithPasswords(t *testing.T) {
	p := withPasswords(t)

	_, started := p.auth.StartSignIn(t.Context(), "", "")
	_, verified := p.auth.SignInWithIDToken(t.Context(), "an-id-token")
	_, finished := p.auth.FinishSignIn(t.Context(), state, state, code)

	assert.ErrorIs(t, started, murphy.ErrForbidden)
	assert.ErrorIs(t, verified, murphy.ErrForbidden)
	assert.ErrorIs(t, finished, murphy.ErrForbidden)
}

func TestTheConfiguredPasswordMakesTheInitialAdminTheFirstAdmin(t *testing.T) {
	p := withPasswords(t)

	signedIn, err := p.auth.SignInWithPassword(t.Context(), initialAdmin, "the configured admin password", address)

	require.NoError(t, err)
	require.Len(t, p.created, 1)
	assert.Equal(t, p.created[0].GetId(), signedIn.Session.GetUserId())
	assert.Equal(t, murphy.UserRole_USER_ROLE_ADMIN, p.created[0].GetRole())
	assert.True(t, strings.HasPrefix(p.hashes[p.created[0].GetId()], "$argon2id$"), "only a hash is kept")
}

func TestTheConfiguredPasswordDoesNothingOnceThereIsAnAdmin(t *testing.T) {
	p := withPasswords(t)
	p.admin = murphy.NewID()

	_, err := p.auth.SignInWithPassword(t.Context(), initialAdmin, "the configured admin password", address)

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Empty(t, p.hashes)
}

func TestTheInitialAdminEmailNeedsTheConfiguredPassword(t *testing.T) {
	p := withPasswords(t)

	_, err := p.auth.SignInWithPassword(t.Context(), initialAdmin, "a guess at the admin password", address)

	assert.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Empty(t, p.created)
}
