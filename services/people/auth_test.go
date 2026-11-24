package people_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/people"
)

const (
	issuer       = "https://id.example.com"
	initialAdmin = "ana@example.com"
	state        = "state-from-the-browser"
	code         = "code-from-the-provider"
	nonce        = "nonce-of-the-sign-in"
)

type accounts struct {
	auth       *people.Auth
	claims     *people.Claims
	signIn     *murphy.SignIn
	identities map[string]string
	users      map[string]*murphy.User
	sessions   map[string]*murphy.Session
	started    *murphy.SignIn
	signInURL  []string
	created    []*murphy.User
	linked     []*murphy.Identity
	session    *murphy.Session
	claimed    []string
}

func hashOf(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func newAccounts(t *testing.T) *accounts {
	t.Helper()
	repo, provider := NewMockAuthRepository(t), NewMockProvider(t)
	a := &accounts{
		auth:       people.NewAuth(repo, provider, time.Hour, initialAdmin),
		claims:     &people.Claims{Issuer: issuer, Subject: "bo-at-the-provider", Email: "bo@example.com", EmailVerified: true, Name: "Bo", Nonce: nonce},
		signIn:     &murphy.SignIn{StateHash: hashOf(state), Nonce: nonce, Verifier: "verifier-of-the-sign-in"},
		identities: map[string]string{},
		users:      map[string]*murphy.User{},
		sessions:   map[string]*murphy.Session{},
	}
	repo.EXPECT().StartSignIn(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, signIn *murphy.SignIn, _ time.Time) error {
		a.started = signIn
		return nil
	}).Maybe()
	provider.EXPECT().SignInURL(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, state, nonce, verifier string) (string, error) {
		a.signInURL = []string{state, nonce, verifier}
		return "https://id.example.com/auth", nil
	}).Maybe()
	repo.EXPECT().TakeSignIn(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, stateHash []byte, _ time.Time) (*murphy.SignIn, error) {
		if string(stateHash) != string(a.signIn.GetStateHash()) {
			return nil, &murphy.NotFoundError{Entity: "sign-in"}
		}
		return a.signIn, nil
	}).Maybe()
	provider.EXPECT().Exchange(mock.Anything, code, mock.Anything).RunAndReturn(func(_ context.Context, _, verifier string) (*people.Claims, error) {
		assert.Equal(t, a.signIn.GetVerifier(), verifier, "the code is exchanged with the sign-in's verifier")
		return a.claims, nil
	}).Maybe()
	provider.EXPECT().Verify(mock.Anything, "id-token").RunAndReturn(func(context.Context, string) (*people.Claims, error) {
		return a.claims, nil
	}).Maybe()
	repo.EXPECT().GetIdentity(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, issuer, subject string) (*murphy.Identity, error) {
		userID, ok := a.identities[issuer+" "+subject]
		if !ok {
			return nil, &murphy.NotFoundError{Entity: "identity"}
		}
		return &murphy.Identity{UserId: userID, Issuer: issuer, Subject: subject}, nil
	}).Maybe()
	repo.EXPECT().GetUserByEmail(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, email string) (*murphy.User, error) {
		user, ok := a.users[email]
		if !ok {
			return nil, &murphy.NotFoundError{Entity: "user"}
		}
		return user, nil
	}).Maybe()
	repo.EXPECT().CreateIdentity(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, identity *murphy.Identity) error {
		a.linked = append(a.linked, identity)
		return nil
	}).Maybe()
	repo.EXPECT().CreateUserWithIdentity(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, user *murphy.User, identity *murphy.Identity) error {
		a.created = append(a.created, user)
		a.linked = append(a.linked, identity)
		return nil
	}).Maybe()
	repo.EXPECT().ClaimFirstAdmin(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, userID string, _ time.Time) (bool, error) {
		a.claimed = append(a.claimed, userID)
		return true, nil
	}).Maybe()
	repo.EXPECT().CreateSession(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, session *murphy.Session) error {
		a.session = session
		return nil
	}).Maybe()
	repo.EXPECT().GetActiveSession(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, tokenHash []byte, _ time.Time) (*murphy.Session, error) {
		session, ok := a.sessions[string(tokenHash)]
		if !ok {
			return nil, &murphy.NotFoundError{Entity: "session"}
		}
		return session, nil
	}).Maybe()
	return a
}

func (a *accounts) known(email, subject string) *murphy.User {
	user := &murphy.User{Id: murphy.NewID(), Email: email}
	a.users[email] = user
	a.identities[issuer+" "+subject] = user.GetId()
	return user
}

func (a *accounts) unlinked(email string) *murphy.User {
	user := &murphy.User{Id: murphy.NewID(), Email: email}
	a.users[email] = user
	return user
}

func TestStartingASignInKeepsOnlyTheStateHash(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.StartSignIn(t.Context())

	require.NoError(t, err)
	require.Len(t, a.signInURL, 3)
	assert.Equal(t, hashOf(a.signInURL[0]), a.started.GetStateHash())
	assert.Equal(t, a.signInURL[1], a.started.GetNonce())
	assert.Equal(t, a.signInURL[2], a.started.GetVerifier())
	assert.GreaterOrEqual(t, len(a.started.GetVerifier()), 43, "PKCE needs at least 43 characters")
}

func TestSomeoneNewBecomesAMember(t *testing.T) {
	a := newAccounts(t)

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, code)

	require.NoError(t, err)
	require.Len(t, a.created, 1)
	assert.Equal(t, "bo@example.com", a.created[0].GetEmail())
	assert.Equal(t, "Bo", a.created[0].GetName())
	assert.Equal(t, murphy.UserRole_USER_ROLE_MEMBER, a.created[0].GetRole())
	assert.Equal(t, a.created[0].GetId(), a.linked[0].GetUserId())
	assert.Equal(t, a.created[0].GetId(), signedIn.Session.GetUserId())
}

func TestAReturningPersonSignsInAsThemselves(t *testing.T) {
	a := newAccounts(t)
	bo := a.known("bo@example.com", "bo-at-the-provider")

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, code)

	require.NoError(t, err)
	assert.Equal(t, bo.GetId(), signedIn.Session.GetUserId())
	assert.Empty(t, a.created)
	assert.Empty(t, a.linked)
}

func TestAVerifiedEmailSignsInToTheUserWithThatEmail(t *testing.T) {
	a := newAccounts(t)
	bo := a.unlinked("bo@example.com")

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, code)

	require.NoError(t, err)
	assert.Equal(t, bo.GetId(), signedIn.Session.GetUserId())
	assert.Empty(t, a.created)
	require.Len(t, a.linked, 1)
	assert.Equal(t, "bo-at-the-provider", a.linked[0].GetSubject())
}

func TestAnUnverifiedEmailNeverSignsInToSomeoneElse(t *testing.T) {
	a := newAccounts(t)
	bo := a.unlinked("bo@example.com")
	a.claims.EmailVerified = false

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, code)

	require.NoError(t, err)
	assert.NotEqual(t, bo.GetId(), signedIn.Session.GetUserId())
	assert.Len(t, a.created, 1)
}

func TestTheProvidersAnswerMustBelongToTheSignIn(t *testing.T) {
	a := newAccounts(t)
	a.claims.Nonce = "nonce-of-another-sign-in"

	_, err := a.auth.FinishSignIn(t.Context(), state, code)

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Nil(t, a.session)
}

func TestAnExpiredOrUsedSignInIsRefused(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.FinishSignIn(t.Context(), "state-of-a-sign-in-already-taken", code)

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Nil(t, a.session)
}

func TestTheInitialAdminClaimsTheFirstAdmin(t *testing.T) {
	a := newAccounts(t)
	ana := a.known(initialAdmin, "ana-at-the-provider")
	a.claims.Subject, a.claims.Email = "ana-at-the-provider", "ANA@example.com"

	_, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.NoError(t, err)
	assert.Equal(t, []string{ana.GetId()}, a.claimed)
}

func TestAnUnverifiedInitialAdminEmailClaimsNothing(t *testing.T) {
	a := newAccounts(t)
	a.claims.Email, a.claims.EmailVerified = initialAdmin, false

	_, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.NoError(t, err)
	assert.Empty(t, a.claimed)
}

func TestEveryoneElseClaimsNothing(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.NoError(t, err)
	assert.Empty(t, a.claimed)
}

func TestASessionIsKeptOnlyAsTheHashOfItsToken(t *testing.T) {
	a := newAccounts(t)

	signedIn, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.NoError(t, err)
	assert.Equal(t, hashOf(signedIn.Token), a.session.GetTokenHash())
	assert.WithinDuration(t, time.Now().Add(time.Hour), a.session.GetExpiresAt().AsTime(), time.Minute)
}

func TestAnActiveSessionAuthenticates(t *testing.T) {
	a := newAccounts(t)
	session := &murphy.Session{Id: murphy.NewID(), UserId: murphy.NewID(), ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}
	a.sessions[string(hashOf("mss_token"))] = session

	got, err := a.auth.Authenticate(t.Context(), "mss_token")

	require.NoError(t, err)
	assert.Equal(t, session.GetUserId(), got.GetUserId())
}

func TestAnUnknownSessionIsRefused(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.Authenticate(t.Context(), "mss_ended-or-never-issued")

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
}
