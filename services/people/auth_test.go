package people_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
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
	repo       *MockAuthRepository
	provider   *MockProvider
	claims     *people.Claims
	signIn     *murphy.SignIn
	identities map[string]string
	users      map[string]*murphy.User
	sessions   map[string]*murphy.Session
	codes      map[string]*murphy.OneTimeCode
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
		auth:       people.NewAuth(repo, provider, NewMockLimiter(t), settings()),
		repo:       repo,
		provider:   provider,
		claims:     &people.Claims{Issuer: issuer, Subject: "bo-at-the-provider", Email: "bo@example.com", EmailVerified: true, Name: "Bo", Nonce: nonce},
		signIn:     &murphy.SignIn{StateHash: hashOf(state), Nonce: nonce, Verifier: "verifier-of-the-sign-in"},
		identities: map[string]string{},
		users:      map[string]*murphy.User{},
		sessions:   map[string]*murphy.Session{},
		codes:      map[string]*murphy.OneTimeCode{},
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
	repo.EXPECT().CreateOneTimeCode(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, code *murphy.OneTimeCode) error {
		a.codes[string(code.GetCodeHash())] = code
		return nil
	}).Maybe()
	repo.EXPECT().TakeOneTimeCode(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, codeHash []byte, _ time.Time) (*murphy.OneTimeCode, error) {
		code, ok := a.codes[string(codeHash)]
		if !ok {
			return nil, &murphy.NotFoundError{Entity: "one-time code"}
		}
		delete(a.codes, string(codeHash))
		return code, nil
	}).Maybe()
	repo.EXPECT().GetActiveSession(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, tokenHash []byte, _ time.Time) (*murphy.Session, murphy.UserRole, error) {
		session, ok := a.sessions[string(tokenHash)]
		if !ok {
			return nil, 0, &murphy.NotFoundError{Entity: "session"}
		}
		return session, murphy.UserRole_USER_ROLE_MEMBER, nil
	}).Maybe()
	return a
}

func settings() people.AuthSettings {
	return people.AuthSettings{
		BrowserSessions:   people.SessionTimeouts{Idle: time.Hour, Absolute: 8 * time.Hour},
		AppSessions:       people.SessionTimeouts{Idle: 24 * time.Hour, Absolute: 30 * 24 * time.Hour},
		InitialAdminEmail: initialAdmin, ReturnOrigins: []string{"https://murphy.example.com"},
		PublicURL: "https://murphy.example.com", AppScheme: "murphy",
	}
}

func (a *accounts) allowingOnly(domains ...string) {
	allowed := settings()
	allowed.AllowedEmailDomains = domains
	a.auth = people.NewAuth(a.repo, a.provider, nil, allowed)
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

	started, err := a.auth.StartSignIn(t.Context(), "", "")

	require.NoError(t, err)
	require.Len(t, a.signInURL, 3)
	assert.Equal(t, a.signInURL[0], started.State, "the browser keeps the state the provider sends back")
	assert.Equal(t, hashOf(a.signInURL[0]), a.started.GetStateHash())
	assert.Equal(t, a.signInURL[1], a.started.GetNonce())
	assert.Equal(t, a.signInURL[2], a.started.GetVerifier())
	assert.GreaterOrEqual(t, len(a.started.GetVerifier()), 43, "PKCE needs at least 43 characters")
}

func TestASignInReturnsToAnAllowedOrigin(t *testing.T) {
	a := newAccounts(t)
	a.signIn.ReturnTo = "https://murphy.example.com/incidents?status=triggered"

	_, err := a.auth.StartSignIn(t.Context(), "https://MURPHY.example.com/incidents?status=triggered", "")
	require.NoError(t, err)
	signedIn, err := a.auth.FinishSignIn(t.Context(), state, state, code)

	require.NoError(t, err)
	assert.Equal(t, "https://MURPHY.example.com/incidents?status=triggered", a.started.GetReturnTo())
	assert.Equal(t, "https://murphy.example.com/incidents?status=triggered", signedIn.ReturnTo)
}

func TestASignInNeverReturnsToAnotherOrigin(t *testing.T) {
	a := newAccounts(t)

	for _, returnTo := range []string{"https://evil.example/", "https://murphy.example.com.evil.example/", "//evil.example/", "https://user@murphy.example.com/", "javascript:alert(1)"} {
		_, err := a.auth.StartSignIn(t.Context(), returnTo, "")

		assert.ErrorIs(t, err, murphy.ErrInvalid, returnTo)
	}
	assert.Nil(t, a.started)
}

func TestSomeoneNewBecomesAMember(t *testing.T) {
	a := newAccounts(t)

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, state, code)

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

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, state, code)

	require.NoError(t, err)
	assert.Equal(t, bo.GetId(), signedIn.Session.GetUserId())
	assert.Empty(t, a.created)
	assert.Empty(t, a.linked)
}

func TestAVerifiedEmailSignsInToTheUserWithThatEmail(t *testing.T) {
	a := newAccounts(t)
	bo := a.unlinked("bo@example.com")

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, state, code)

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

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, state, code)

	require.NoError(t, err)
	assert.NotEqual(t, bo.GetId(), signedIn.Session.GetUserId())
	assert.Len(t, a.created, 1)
}

func TestASignInOnlyFinishesInTheBrowserThatStartedIt(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.FinishSignIn(t.Context(), state, "state-of-the-victims-own-sign-in", code)

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Nil(t, a.session)
}

func TestTheProvidersAnswerMustBelongToTheSignIn(t *testing.T) {
	a := newAccounts(t)
	a.claims.Nonce = "nonce-of-another-sign-in"

	_, err := a.auth.FinishSignIn(t.Context(), state, state, code)

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Nil(t, a.session)
}

func TestAnExpiredOrUsedSignInIsRefused(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.FinishSignIn(t.Context(), "state-of-a-sign-in-already-taken", "state-of-a-sign-in-already-taken", code)

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
}

func TestAnActiveSessionAuthenticates(t *testing.T) {
	a := newAccounts(t)
	session := a.activeSession(23*time.Hour, 30*24*time.Hour)

	got, role, err := a.auth.Authenticate(t.Context(), "mss_token")

	require.NoError(t, err)
	assert.Equal(t, session.GetUserId(), got.GetUserId())
	assert.Equal(t, murphy.UserRole_USER_ROLE_MEMBER, role)
}

func (a *accounts) activeSession(expiresIn, renewableFor time.Duration) *murphy.Session {
	now := time.Now()
	session := &murphy.Session{
		Id: murphy.NewID(), UserId: murphy.NewID(), Client: murphy.SessionClient_SESSION_CLIENT_APP,
		ExpiresAt: timestamppb.New(now.Add(expiresIn)), RenewableUntil: timestamppb.New(now.Add(renewableFor)),
	}
	a.sessions[string(hashOf("mss_token"))] = session
	return session
}

func (a *accounts) renewals() *[]time.Time {
	renewed := &[]time.Time{}
	a.repo.EXPECT().RenewSession(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, _ string, expiresAt, _ time.Time) error {
		*renewed = append(*renewed, expiresAt)
		return nil
	}).Maybe()
	return renewed
}

func TestBrowserAndAppSessionsLastTheirOwnTimeouts(t *testing.T) {
	a := newAccounts(t)
	_, err := a.auth.FinishSignIn(t.Context(), state, state, code)
	require.NoError(t, err)
	browser := a.session
	connect, err := a.auth.CreateConnectCode(t.Context(), murphy.NewID())
	require.NoError(t, err)

	_, err = a.auth.SignInWithCode(t.Context(), connect.Code, "")

	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), browser.GetExpiresAt().AsTime(), time.Minute)
	assert.WithinDuration(t, time.Now().Add(8*time.Hour), browser.GetRenewableUntil().AsTime(), time.Minute)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), a.session.GetExpiresAt().AsTime(), time.Minute)
	assert.WithinDuration(t, time.Now().Add(30*24*time.Hour), a.session.GetRenewableUntil().AsTime(), time.Minute)
}

func TestASessionInUseIsRenewed(t *testing.T) {
	a := newAccounts(t)
	renewed := a.renewals()
	a.activeSession(time.Hour, 30*24*time.Hour)

	got, _, err := a.auth.Authenticate(t.Context(), "mss_token")

	require.NoError(t, err)
	require.Len(t, *renewed, 1)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), (*renewed)[0], time.Minute)
	assert.Equal(t, (*renewed)[0], got.GetExpiresAt().AsTime(), "the caller sees the new expiry")
}

func TestASessionIsRenewedOnlyOnceHalfItsIdleTimeoutHasPassed(t *testing.T) {
	a := newAccounts(t)
	renewed := a.renewals()
	a.activeSession(13*time.Hour, 30*24*time.Hour)

	_, _, err := a.auth.Authenticate(t.Context(), "mss_token")

	require.NoError(t, err)
	assert.Empty(t, *renewed)
}

func TestRenewingNeverPassesTheAbsoluteTimeout(t *testing.T) {
	a := newAccounts(t)
	renewed := a.renewals()
	session := a.activeSession(time.Hour, 20*time.Hour)

	_, _, err := a.auth.Authenticate(t.Context(), "mss_token")

	require.NoError(t, err)
	assert.Equal(t, []time.Time{session.GetRenewableUntil().AsTime()}, *renewed)
}

func TestAnUnknownSessionIsRefused(t *testing.T) {
	a := newAccounts(t)

	_, _, err := a.auth.Authenticate(t.Context(), "mss_ended-or-never-issued")

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
}

const (
	verifier  = "a-verifier-only-the-app-knows-and-at-least-43-characters-long"
	appReturn = "murphy://signed-in"
)

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func codeIn(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	return parsed.Query().Get("code")
}

func TestTheAppsSignInNeedsAPKCEChallenge(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.StartSignIn(t.Context(), appReturn, "")

	assert.ErrorIs(t, err, murphy.ErrInvalid)
	assert.Nil(t, a.started)
}

func TestOnlyTheAppsSignInTakesAPKCEChallenge(t *testing.T) {
	a := newAccounts(t)

	_, err := a.auth.StartSignIn(t.Context(), "", challengeOf(verifier))

	assert.ErrorIs(t, err, murphy.ErrInvalid)
}

func TestTheAppGetsAOneTimeCodeInsteadOfASession(t *testing.T) {
	a := newAccounts(t)
	a.signIn.ReturnTo, a.signIn.CodeChallenge = appReturn, challengeOf(verifier)

	signedIn, err := a.auth.FinishSignIn(t.Context(), state, state, code)

	require.NoError(t, err)
	assert.Nil(t, signedIn.Session)
	assert.Nil(t, a.session)
	assert.Regexp(t, `^murphy://signed-in\?code=mas_`, signedIn.ReturnTo)
}

func TestTheAppTradesItsCodeAndVerifierForASession(t *testing.T) {
	a := newAccounts(t)
	a.signIn.ReturnTo, a.signIn.CodeChallenge = appReturn, challengeOf(verifier)
	handedOver, err := a.auth.FinishSignIn(t.Context(), state, state, code)
	require.NoError(t, err)

	signedIn, err := a.auth.SignInWithCode(t.Context(), codeIn(t, handedOver.ReturnTo), verifier)

	require.NoError(t, err)
	assert.Equal(t, a.created[0].GetId(), signedIn.Session.GetUserId())
}

func TestAnInterceptedAppCodeIsUselessWithoutTheVerifier(t *testing.T) {
	a := newAccounts(t)
	a.signIn.ReturnTo, a.signIn.CodeChallenge = appReturn, challengeOf(verifier)
	handedOver, err := a.auth.FinishSignIn(t.Context(), state, state, code)
	require.NoError(t, err)

	_, err = a.auth.SignInWithCode(t.Context(), codeIn(t, handedOver.ReturnTo), "a-guessed-verifier")

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
	assert.Nil(t, a.session)
}

func TestAConnectCodeSignsTheAppInAsWhoeverCreatedIt(t *testing.T) {
	a := newAccounts(t)
	bo := murphy.NewID()
	connect, err := a.auth.CreateConnectCode(t.Context(), bo)
	require.NoError(t, err)

	signedIn, err := a.auth.SignInWithCode(t.Context(), connect.Code, "")

	require.NoError(t, err)
	assert.Equal(t, bo, signedIn.Session.GetUserId())
	assert.Equal(t, "murphy://connect?code="+url.QueryEscape(connect.Code)+"&server=https%3A%2F%2Fmurphy.example.com", connect.Link)
	assert.WithinDuration(t, time.Now().Add(45*time.Second), connect.ExpiresAt, 5*time.Second)
}

func TestAOneTimeCodeWorksOnce(t *testing.T) {
	a := newAccounts(t)
	connect, err := a.auth.CreateConnectCode(t.Context(), murphy.NewID())
	require.NoError(t, err)
	_, err = a.auth.SignInWithCode(t.Context(), connect.Code, "")
	require.NoError(t, err)

	_, err = a.auth.SignInWithCode(t.Context(), connect.Code, "")

	require.ErrorIs(t, err, murphy.ErrUnauthenticated)
}

func TestEachSessionRemembersHowItStarted(t *testing.T) {
	a := newAccounts(t)
	_, err := a.auth.FinishSignIn(t.Context(), state, state, code)
	require.NoError(t, err)
	browser := a.session.GetClient()
	_, err = a.auth.SignInWithIDToken(t.Context(), "id-token")
	require.NoError(t, err)
	token := a.session.GetClient()
	connect, err := a.auth.CreateConnectCode(t.Context(), murphy.NewID())
	require.NoError(t, err)

	_, err = a.auth.SignInWithCode(t.Context(), connect.Code, "")

	require.NoError(t, err)
	assert.Equal(t, murphy.SessionClient_SESSION_CLIENT_BROWSER, browser)
	assert.Equal(t, murphy.SessionClient_SESSION_CLIENT_TOKEN, token)
	assert.Equal(t, murphy.SessionClient_SESSION_CLIENT_APP, a.session.GetClient())
}

func TestSomeoneNewOutsideTheAllowedDomainsCannotSignIn(t *testing.T) {
	a := newAccounts(t)
	a.allowingOnly("murphy.example.com")

	_, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.ErrorIs(t, err, murphy.ErrForbidden)
	assert.Empty(t, a.created)
	assert.Nil(t, a.session)
}

func TestSomeoneNewInAnAllowedDomainSignsIn(t *testing.T) {
	a := newAccounts(t)
	a.allowingOnly("murphy.example.com", "EXAMPLE.com")

	_, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.NoError(t, err)
	assert.Len(t, a.created, 1)
}

func TestSomeoneAnAdminCreatedSignsInFromAnyDomain(t *testing.T) {
	a := newAccounts(t)
	a.allowingOnly("murphy.example.com")
	bo := a.unlinked("bo@example.com")

	signedIn, err := a.auth.SignInWithIDToken(t.Context(), "id-token")

	require.NoError(t, err)
	assert.Equal(t, bo.GetId(), signedIn.Session.GetUserId())
}
