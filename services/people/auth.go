package people

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const (
	signInDuration       = 10 * time.Minute
	appSignInDuration    = 2 * time.Minute
	connectDuration      = 45 * time.Second
	sessionPrefix        = "mss_"
	appSignInPrefix      = "mas_"
	connectCodePrefix    = "mcc_"
	passwordLinkPrefix   = "mpl_"
	passwordLinkDuration = 24 * time.Hour
	failureWindow        = 15 * time.Minute
	maxPersonFailures    = 5
	maxAddressFailures   = 50
	appSignedInPath      = "://signed-in"
	appConnectPath       = "://connect"
	codeChallengeChars   = 43
)

type Claims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Nonce         string
}

type Provider interface {
	SignInURL(ctx context.Context, state, nonce, verifier string) (string, error)
	Exchange(ctx context.Context, code, verifier string) (*Claims, error)
	Verify(ctx context.Context, idToken string) (*Claims, error)
}

type AuthRepository interface {
	StartSignIn(ctx context.Context, signIn *murphy.SignIn, now time.Time) error
	TakeSignIn(ctx context.Context, stateHash []byte, now time.Time) (*murphy.SignIn, error)
	GetIdentity(ctx context.Context, issuer, subject string) (*murphy.Identity, error)
	GetUserByEmail(ctx context.Context, email string) (*murphy.User, error)
	CreateIdentity(ctx context.Context, identity *murphy.Identity) error
	CreateUserWithIdentity(ctx context.Context, user *murphy.User, identity *murphy.Identity) error
	CreateSession(ctx context.Context, session *murphy.Session) error
	GetActiveSession(ctx context.Context, tokenHash []byte, now time.Time) (*murphy.Session, murphy.UserRole, error)
	RenewSession(ctx context.Context, id string, expiresAt, at time.Time) error
	RevokeSession(ctx context.Context, tokenHash []byte, at time.Time) error
	ClaimFirstAdmin(ctx context.Context, userID string, now time.Time) (bool, error)
	CreateOneTimeCode(ctx context.Context, code *murphy.OneTimeCode) error
	TakeOneTimeCode(ctx context.Context, codeHash []byte, now time.Time) (*murphy.OneTimeCode, error)
	ListActiveSessions(ctx context.Context, userID, before string, limit int, now time.Time) ([]*murphy.Session, error)
	RevokeSessionOfUser(ctx context.Context, userID, id string, at time.Time) error
	EndSessionsOfUser(ctx context.Context, userID string, at time.Time) error
	SetPassword(ctx context.Context, userID, hash string, at time.Time) error
	GetPasswordHash(ctx context.Context, userID string) (string, error)
	GetUser(ctx context.Context, id string) (*murphy.User, error)
	CreateUser(ctx context.Context, user *murphy.User) error
}

type Limiter interface {
	Hit(ctx context.Context, key string, window time.Duration) (int64, error)
	Count(ctx context.Context, key string) (int64, error)
	Reset(ctx context.Context, key string) error
}

type SessionTimeouts struct {
	Idle     time.Duration
	Absolute time.Duration
}

type AuthSettings struct {
	BrowserSessions      SessionTimeouts
	AppSessions          SessionTimeouts
	InitialAdminEmail    string
	ReturnOrigins        []string
	PublicURL            string
	AppScheme            string
	AllowedEmailDomains  []string
	PasswordsAllowed     bool
	InitialAdminPassword string
}

type Auth struct {
	repo          AuthRepository
	provider      Provider
	limiter       Limiter
	settings      AuthSettings
	returnOrigins map[string]bool
}

var (
	errNoProvider = &murphy.ForbiddenError{Reason: "this install has no identity provider; sign in with a password"}
	errNoPassword = &murphy.ForbiddenError{Reason: "this install signs in through its identity provider, not with passwords"}
)

func NewAuth(repo AuthRepository, provider Provider, limiter Limiter, settings AuthSettings) *Auth {
	origins := make(map[string]bool, len(settings.ReturnOrigins))
	for _, origin := range settings.ReturnOrigins {
		origins[strings.ToLower(strings.TrimSuffix(origin, "/"))] = true
	}
	return &Auth{repo: repo, provider: provider, limiter: limiter, settings: settings, returnOrigins: origins}
}

type StartedSignIn struct {
	URL   string
	State string
}

type ConnectCode struct {
	Code      string
	Link      string
	ExpiresAt time.Time
}

type SignedIn struct {
	Token    string
	Session  *murphy.Session
	ReturnTo string
}

func (a *Auth) StartSignIn(ctx context.Context, returnTo, codeChallenge string) (_ *StartedSignIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if a.provider == nil {
		return nil, errNoProvider
	}
	if err := a.checkReturnTo(returnTo, codeChallenge); err != nil {
		return nil, err
	}
	state, nonce, verifier := secret(), secret(), secret()
	now := time.Now().UTC()
	signIn := &murphy.SignIn{
		Id:            murphy.NewID(),
		StateHash:     hash(state),
		Nonce:         nonce,
		Verifier:      verifier,
		ReturnTo:      returnTo,
		CodeChallenge: codeChallenge,
		ExpiresAt:     timestamppb.New(now.Add(signInDuration)),
		CreatedAt:     timestamppb.New(now),
		UpdatedAt:     timestamppb.New(now),
	}
	if err := a.repo.StartSignIn(ctx, signIn, now); err != nil {
		return nil, fmt.Errorf("start sign-in: %w", err)
	}
	url, err := a.provider.SignInURL(ctx, state, nonce, verifier)
	if err != nil {
		return nil, err
	}
	return &StartedSignIn{URL: url, State: state}, nil
}

func (a *Auth) FinishSignIn(ctx context.Context, state, browserState, code string) (_ *SignedIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if a.provider == nil {
		return nil, errNoProvider
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(browserState)) != 1 {
		return nil, fmt.Errorf("%w: the sign-in was started in another browser", murphy.ErrUnauthenticated)
	}
	now := time.Now().UTC()
	signIn, err := a.repo.TakeSignIn(ctx, hash(state), now)
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, fmt.Errorf("%w: the sign-in expired or was already used", murphy.ErrUnauthenticated)
	}
	if err != nil {
		return nil, fmt.Errorf("take sign-in: %w", err)
	}
	claims, err := a.provider.Exchange(ctx, code, signIn.GetVerifier())
	if err != nil {
		return nil, err
	}
	if claims.Nonce != signIn.GetNonce() {
		return nil, fmt.Errorf("%w: the identity provider answered a different sign-in", murphy.ErrUnauthenticated)
	}
	ctx, userID, err := a.signedInUser(ctx, claims, now)
	if err != nil {
		return nil, err
	}
	if signIn.GetCodeChallenge() != "" {
		return a.handToApp(ctx, signIn, userID, now)
	}
	signedIn, err := a.startSession(ctx, userID, murphy.SessionClient_SESSION_CLIENT_BROWSER, now)
	if err != nil {
		return nil, err
	}
	signedIn.ReturnTo = signIn.GetReturnTo()
	return signedIn, nil
}

func (a *Auth) SignInWithIDToken(ctx context.Context, idToken string) (_ *SignedIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if a.provider == nil {
		return nil, errNoProvider
	}
	claims, err := a.provider.Verify(ctx, idToken)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ctx, userID, err := a.signedInUser(ctx, claims, now)
	if err != nil {
		return nil, err
	}
	return a.startSession(ctx, userID, murphy.SessionClient_SESSION_CLIENT_TOKEN, now)
}

func (a *Auth) SignInWithCode(ctx context.Context, code, codeVerifier string) (_ *SignedIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := time.Now().UTC()
	taken, err := a.repo.TakeOneTimeCode(ctx, hash(code), now)
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, fmt.Errorf("%w: the code expired or was already used", murphy.ErrUnauthenticated)
	}
	if err != nil {
		return nil, fmt.Errorf("take one-time code: %w", err)
	}
	if taken.GetPurpose() == murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_SET_PASSWORD {
		return nil, fmt.Errorf("%w: a set-password link doesn't sign anyone in", murphy.ErrUnauthenticated)
	}
	if taken.GetCodeChallenge() != "" && subtle.ConstantTimeCompare([]byte(challengeOf(codeVerifier)), []byte(taken.GetCodeChallenge())) != 1 {
		return nil, fmt.Errorf("%w: the code verifier doesn't match the sign-in", murphy.ErrUnauthenticated)
	}
	ctx = span.Set(ctx, telemetry.WithField("user.id", taken.GetUserId()))
	return a.startSession(ctx, taken.GetUserId(), murphy.SessionClient_SESSION_CLIENT_APP, now)
}

func (a *Auth) ListSessions(ctx context.Context, userID string, page pagination.Page) (_ []*murphy.Session, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	sessions, err := a.repo.ListActiveSessions(ctx, userID, window.Before, window.Rows(), time.Now().UTC())
	if err != nil {
		return nil, "", fmt.Errorf("list sessions: %w", err)
	}
	items, next := pagination.Trim(sessions, window)
	return items, next, nil
}

func (a *Auth) EndSession(ctx context.Context, userID, sessionID string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	if err := murphy.LookupID("session", sessionID); err != nil {
		return err
	}
	if err := a.repo.RevokeSessionOfUser(ctx, userID, sessionID, time.Now().UTC()); err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

func (a *Auth) EndAllSessions(ctx context.Context, userID string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	if err := a.repo.EndSessionsOfUser(ctx, userID, time.Now().UTC()); err != nil {
		return fmt.Errorf("end all sessions: %w", err)
	}
	return nil
}

func (a *Auth) CreateConnectCode(ctx context.Context, userID string) (_ *ConnectCode, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	now := time.Now().UTC()
	code, err := a.createCode(ctx, connectCodePrefix, murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_CONNECT, userID, "", now.Add(connectDuration), now)
	if err != nil {
		return nil, err
	}
	link := a.settings.AppScheme + appConnectPath + "?" + url.Values{"server": {a.settings.PublicURL}, "code": {code}}.Encode()
	return &ConnectCode{Code: code, Link: link, ExpiresAt: now.Add(connectDuration)}, nil
}

func (a *Auth) Authenticate(ctx context.Context, token string) (_ *murphy.Session, _ murphy.UserRole, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	session, role, err := a.repo.GetActiveSession(ctx, hash(token), time.Now().UTC())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, 0, murphy.ErrUnauthenticated
	}
	if err != nil {
		return nil, 0, fmt.Errorf("get session: %w", err)
	}
	ctx = span.Set(ctx, telemetry.WithField("user.id", session.GetUserId()))
	if err := a.renew(ctx, session, time.Now().UTC()); err != nil {
		return nil, 0, err
	}
	return session, role, nil
}

func (a *Auth) renew(ctx context.Context, session *murphy.Session, now time.Time) error {
	idle := a.timeouts(session.GetClient()).Idle
	renewed := now.Add(idle)
	if renewableUntil := session.GetRenewableUntil().AsTime(); renewed.After(renewableUntil) {
		renewed = renewableUntil
	}
	if renewed.Sub(session.GetExpiresAt().AsTime()) < idle/2 {
		return nil
	}
	if err := a.repo.RenewSession(ctx, session.GetId(), renewed, now); err != nil {
		return fmt.Errorf("renew session: %w", err)
	}
	session.ExpiresAt, session.UpdatedAt = timestamppb.New(renewed), timestamppb.New(now)
	return nil
}

func (a *Auth) timeouts(client murphy.SessionClient) SessionTimeouts {
	if client == murphy.SessionClient_SESSION_CLIENT_BROWSER {
		return a.settings.BrowserSessions
	}
	return a.settings.AppSessions
}

func (a *Auth) SignOut(ctx context.Context, token string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	err = a.repo.RevokeSession(ctx, hash(token), time.Now().UTC())
	if errors.Is(err, murphy.ErrNotFound) {
		return murphy.ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (a *Auth) userFor(ctx context.Context, claims *Claims, now time.Time) (string, error) {
	identity, err := a.repo.GetIdentity(ctx, claims.Issuer, claims.Subject)
	if err == nil {
		return identity.GetUserId(), nil
	}
	if !errors.Is(err, murphy.ErrNotFound) {
		return "", fmt.Errorf("get identity: %w", err)
	}
	identity = &murphy.Identity{
		Id:        murphy.NewID(),
		Issuer:    claims.Issuer,
		Subject:   claims.Subject,
		CreatedAt: timestamppb.New(now),
		UpdatedAt: timestamppb.New(now),
	}
	if claims.EmailVerified {
		existing, err := a.repo.GetUserByEmail(ctx, claims.Email)
		if err == nil {
			identity.UserId = existing.GetId()
			if err := a.repo.CreateIdentity(ctx, identity); err != nil {
				return "", fmt.Errorf("create identity: %w", err)
			}
			return existing.GetId(), nil
		}
		if !errors.Is(err, murphy.ErrNotFound) {
			return "", fmt.Errorf("find user by email: %w", err)
		}
	}
	if !a.mayJoin(claims.Email) {
		return "", &murphy.ForbiddenError{Reason: "people with this email domain can't sign in to this install"}
	}
	user := &murphy.User{
		Id:        murphy.NewID(),
		Name:      murphy.Or(claims.Name, claims.Email),
		Email:     claims.Email,
		TimeZone:  "UTC",
		Status:    murphy.UserStatus_USER_STATUS_ACTIVE,
		Role:      murphy.UserRole_USER_ROLE_MEMBER,
		CreatedAt: timestamppb.New(now),
		UpdatedAt: timestamppb.New(now),
	}
	if err := user.Validate(); err != nil {
		return "", fmt.Errorf("%w: the identity provider gave no usable name and email", murphy.ErrUnauthenticated)
	}
	identity.UserId = user.GetId()
	if err := a.repo.CreateUserWithIdentity(ctx, user, identity); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	return user.GetId(), nil
}

func (a *Auth) signedInUser(ctx context.Context, claims *Claims, now time.Time) (context.Context, string, error) {
	userID, err := a.userFor(ctx, claims, now)
	if err != nil {
		return ctx, "", err
	}
	ctx = telemetry.With(ctx, telemetry.WithField("user.id", userID))
	if a.settings.InitialAdminEmail == "" || !claims.EmailVerified || !strings.EqualFold(claims.Email, a.settings.InitialAdminEmail) {
		return ctx, userID, nil
	}
	claimed, err := a.repo.ClaimFirstAdmin(ctx, userID, now)
	if err != nil {
		return ctx, "", fmt.Errorf("claim the first admin: %w", err)
	}
	if claimed {
		logger.Info(ctx, "the initial admin signed in and became the first admin")
	}
	return ctx, userID, nil
}

func (a *Auth) startSession(ctx context.Context, userID string, client murphy.SessionClient, now time.Time) (*SignedIn, error) {
	token := sessionPrefix + secret()
	timeouts := a.timeouts(client)
	session := &murphy.Session{
		Id:             murphy.NewID(),
		UserId:         userID,
		TokenHash:      hash(token),
		Client:         client,
		ExpiresAt:      timestamppb.New(now.Add(min(timeouts.Idle, timeouts.Absolute))),
		RenewableUntil: timestamppb.New(now.Add(timeouts.Absolute)),
		CreatedAt:      timestamppb.New(now),
		UpdatedAt:      timestamppb.New(now),
	}
	if err := a.repo.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &SignedIn{Token: token, Session: session}, nil
}

func (a *Auth) handToApp(ctx context.Context, signIn *murphy.SignIn, userID string, now time.Time) (*SignedIn, error) {
	code, err := a.createCode(ctx, appSignInPrefix, murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_APP_SIGN_IN, userID, signIn.GetCodeChallenge(), now.Add(appSignInDuration), now)
	if err != nil {
		return nil, err
	}
	return &SignedIn{ReturnTo: signIn.GetReturnTo() + "?" + url.Values{"code": {code}}.Encode()}, nil
}

func (a *Auth) createCode(ctx context.Context, prefix string, purpose murphy.OneTimeCodePurpose, userID, codeChallenge string, expiresAt, now time.Time) (string, error) {
	code := prefix + secret()
	err := a.repo.CreateOneTimeCode(ctx, &murphy.OneTimeCode{
		Id:            murphy.NewID(),
		CodeHash:      hash(code),
		UserId:        userID,
		Purpose:       purpose,
		CodeChallenge: codeChallenge,
		ExpiresAt:     timestamppb.New(expiresAt),
		CreatedAt:     timestamppb.New(now),
		UpdatedAt:     timestamppb.New(now),
	})
	if err != nil {
		return "", fmt.Errorf("create one-time code: %w", err)
	}
	return code, nil
}

func (a *Auth) checkReturnTo(returnTo, codeChallenge string) error {
	toApp := a.settings.AppScheme != "" && returnTo == a.settings.AppScheme+appSignedInPath
	switch {
	case toApp && !validChallenge(codeChallenge):
		return &murphy.FieldError{Field: "code_challenge", Reason: "must be an S256 PKCE challenge when signing in to the app"}
	case toApp:
		return nil
	case codeChallenge != "":
		return &murphy.FieldError{Field: "code_challenge", Reason: "is only for signing in to the app"}
	case returnTo == "":
		return nil
	}
	u, err := url.Parse(returnTo)
	if err != nil || u.User != nil || !a.returnOrigins[strings.ToLower(u.Scheme+"://"+u.Host)] {
		return &murphy.FieldError{Field: "return_to", Reason: "must be on an origin this install allows"}
	}
	return nil
}

func (a *Auth) mayJoin(email string) bool {
	if len(a.settings.AllowedEmailDomains) == 0 {
		return true
	}
	_, domain, ok := strings.Cut(email, "@")
	return ok && slices.ContainsFunc(a.settings.AllowedEmailDomains, func(allowed string) bool {
		return strings.EqualFold(strings.TrimPrefix(allowed, "@"), domain)
	})
}

func validChallenge(challenge string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(challenge) == codeChallengeChars && len(decoded) == sha256.Size
}

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func secret() string {
	random := make([]byte, 32)
	_, _ = rand.Read(random)
	return base64.RawURLEncoding.EncodeToString(random)
}

func hash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

type PasswordLink struct {
	Link      string
	ExpiresAt time.Time
}

func (a *Auth) SignInWithPassword(ctx context.Context, email, password, address string) (_ *SignedIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if !a.settings.PasswordsAllowed {
		return nil, errNoPassword
	}
	person, place := "sign-in:person:"+strings.ToLower(email), "sign-in:address:"+address
	if err := a.withinLimits(ctx, person, place); err != nil {
		return nil, err
	}
	user, stored, err := a.passwordOf(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil && a.isInitialAdmin(email, password) {
		user, err = a.claimWithPassword(ctx, email, password)
		if err != nil {
			return nil, err
		}
		stored = mustHashPassword(password)
	}
	if !passwordMatches(password, stored) || user == nil {
		if _, err := a.limiter.Hit(ctx, person, failureWindow); err != nil {
			return nil, fmt.Errorf("count a failed sign-in: %w", err)
		}
		if _, err := a.limiter.Hit(ctx, place, failureWindow); err != nil {
			return nil, fmt.Errorf("count a failed sign-in: %w", err)
		}
		return nil, fmt.Errorf("%w: wrong email or password", murphy.ErrUnauthenticated)
	}
	if err := a.limiter.Reset(ctx, person); err != nil {
		return nil, fmt.Errorf("forget failed sign-ins: %w", err)
	}
	ctx = span.Set(ctx, telemetry.WithField("user.id", user.GetId()))
	return a.startSession(ctx, user.GetId(), murphy.SessionClient_SESSION_CLIENT_TOKEN, time.Now().UTC())
}

func (a *Auth) withinLimits(ctx context.Context, person, place string) error {
	personFailures, err := a.limiter.Count(ctx, person)
	if err != nil {
		return fmt.Errorf("count failed sign-ins: %w", err)
	}
	placeFailures, err := a.limiter.Count(ctx, place)
	if err != nil {
		return fmt.Errorf("count failed sign-ins: %w", err)
	}
	if personFailures >= maxPersonFailures || placeFailures >= maxAddressFailures {
		return murphy.ErrTooManyAttempts
	}
	return nil
}

func (a *Auth) passwordOf(ctx context.Context, email string) (*murphy.User, string, error) {
	user, err := a.repo.GetUserByEmail(ctx, email)
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, unusable, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("find user by email: %w", err)
	}
	stored, err := a.repo.GetPasswordHash(ctx, user.GetId())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, unusable, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("get password: %w", err)
	}
	if user.GetStatus() != murphy.UserStatus_USER_STATUS_ACTIVE {
		return nil, stored, nil
	}
	return user, stored, nil
}

func (a *Auth) CreatePasswordLink(ctx context.Context, userID string) (_ *PasswordLink, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	if !a.settings.PasswordsAllowed {
		return nil, errNoPassword
	}
	if err := murphy.LookupID("user", userID); err != nil {
		return nil, err
	}
	if _, err := a.repo.GetUser(ctx, userID); err != nil {
		return nil, err
	}
	return a.passwordLink(ctx, userID, time.Now().UTC())
}

func (a *Auth) passwordLink(ctx context.Context, userID string, now time.Time) (*PasswordLink, error) {
	expiresAt := now.Add(passwordLinkDuration)
	code, err := a.createCode(ctx, passwordLinkPrefix, murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_SET_PASSWORD, userID, "", expiresAt, now)
	if err != nil {
		return nil, err
	}
	// The web UI will show this screen. Until it exists, nothing answers at this address, and the code works only through POST /v1/auth/password.
	return &PasswordLink{Link: a.settings.PublicURL + "/set-password?" + url.Values{"code": {code}}.Encode(), ExpiresAt: expiresAt}, nil
}

func (a *Auth) SetPassword(ctx context.Context, code, password string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if !a.settings.PasswordsAllowed {
		return errNoPassword
	}
	if err := checkPassword(password); err != nil {
		return err
	}
	now := time.Now().UTC()
	taken, err := a.repo.TakeOneTimeCode(ctx, hash(code), now)
	if errors.Is(err, murphy.ErrNotFound) || (err == nil && taken.GetPurpose() != murphy.OneTimeCodePurpose_ONE_TIME_CODE_PURPOSE_SET_PASSWORD) {
		return fmt.Errorf("%w: the link expired or was already used", murphy.ErrUnauthenticated)
	}
	if err != nil {
		return fmt.Errorf("take one-time code: %w", err)
	}
	ctx = span.Set(ctx, telemetry.WithField("user.id", taken.GetUserId()))
	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := a.repo.SetPassword(ctx, taken.GetUserId(), hashed, now); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if err := a.repo.EndSessionsOfUser(ctx, taken.GetUserId(), now); err != nil {
		return fmt.Errorf("end the person's other sessions: %w", err)
	}
	return nil
}

func (a *Auth) isInitialAdmin(email, password string) bool {
	return a.settings.InitialAdminEmail != "" && a.settings.InitialAdminPassword != "" &&
		strings.EqualFold(email, a.settings.InitialAdminEmail) &&
		subtle.ConstantTimeCompare([]byte(password), []byte(a.settings.InitialAdminPassword)) == 1
}

func (a *Auth) claimWithPassword(ctx context.Context, email, password string) (*murphy.User, error) {
	now := time.Now().UTC()
	user, err := a.repo.GetUserByEmail(ctx, email)
	if errors.Is(err, murphy.ErrNotFound) {
		user = &murphy.User{
			Id: murphy.NewID(), Name: email, Email: email, TimeZone: "UTC",
			Status: murphy.UserStatus_USER_STATUS_ACTIVE, Role: murphy.UserRole_USER_ROLE_MEMBER,
			CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		}
		err = a.repo.CreateUser(ctx, user)
	}
	if err != nil {
		return nil, fmt.Errorf("find or create the initial admin: %w", err)
	}
	claimed, err := a.repo.ClaimFirstAdmin(ctx, user.GetId(), now)
	if err != nil {
		return nil, fmt.Errorf("claim the first admin: %w", err)
	}
	if !claimed {
		return nil, nil
	}
	hashed, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	if err := a.repo.SetPassword(ctx, user.GetId(), hashed, now); err != nil {
		return nil, fmt.Errorf("set the first admin's password: %w", err)
	}
	logger.Info(ctx, "the initial admin signed in with the configured password and became the first admin")
	return user, nil
}
