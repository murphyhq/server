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
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const (
	signInDuration = 10 * time.Minute
	sessionPrefix  = "mss_"
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
	GetActiveSession(ctx context.Context, tokenHash []byte, now time.Time) (*murphy.Session, error)
	RevokeSession(ctx context.Context, tokenHash []byte, at time.Time) error
	ClaimFirstAdmin(ctx context.Context, userID string, now time.Time) (bool, error)
}

type AuthSettings struct {
	SessionDuration   time.Duration
	InitialAdminEmail string
	ReturnOrigins     []string
}

type Auth struct {
	repo          AuthRepository
	provider      Provider
	settings      AuthSettings
	returnOrigins map[string]bool
}

func NewAuth(repo AuthRepository, provider Provider, settings AuthSettings) *Auth {
	origins := make(map[string]bool, len(settings.ReturnOrigins))
	for _, origin := range settings.ReturnOrigins {
		origins[strings.ToLower(strings.TrimSuffix(origin, "/"))] = true
	}
	return &Auth{repo: repo, provider: provider, settings: settings, returnOrigins: origins}
}

type StartedSignIn struct {
	URL   string
	State string
}

type SignedIn struct {
	Token    string
	Session  *murphy.Session
	ReturnTo string
}

func (a *Auth) StartSignIn(ctx context.Context, returnTo string) (_ *StartedSignIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := a.checkReturnTo(returnTo); err != nil {
		return nil, err
	}
	state, nonce, verifier := secret(), secret(), secret()
	now := time.Now().UTC()
	signIn := &murphy.SignIn{
		Id:        murphy.NewID(),
		StateHash: hash(state),
		Nonce:     nonce,
		Verifier:  verifier,
		ReturnTo:  returnTo,
		ExpiresAt: timestamppb.New(now.Add(signInDuration)),
		CreatedAt: timestamppb.New(now),
		UpdatedAt: timestamppb.New(now),
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
	signedIn, err := a.startSession(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	signedIn.ReturnTo = signIn.GetReturnTo()
	return signedIn, nil
}

func (a *Auth) SignInWithIDToken(ctx context.Context, idToken string) (_ *SignedIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	claims, err := a.provider.Verify(ctx, idToken)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ctx, userID, err := a.signedInUser(ctx, claims, now)
	if err != nil {
		return nil, err
	}
	return a.startSession(ctx, userID, now)
}

func (a *Auth) Authenticate(ctx context.Context, token string) (_ *murphy.Session, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	session, err := a.repo.GetActiveSession(ctx, hash(token), time.Now().UTC())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, murphy.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	span.Set(ctx, telemetry.WithField("user.id", session.GetUserId()))
	return session, nil
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

func (a *Auth) startSession(ctx context.Context, userID string, now time.Time) (*SignedIn, error) {
	token := sessionPrefix + secret()
	session := &murphy.Session{
		Id:        murphy.NewID(),
		UserId:    userID,
		TokenHash: hash(token),
		ExpiresAt: timestamppb.New(now.Add(a.settings.SessionDuration)),
		CreatedAt: timestamppb.New(now),
		UpdatedAt: timestamppb.New(now),
	}
	if err := a.repo.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &SignedIn{Token: token, Session: session}, nil
}

func (a *Auth) checkReturnTo(returnTo string) error {
	if returnTo == "" {
		return nil
	}
	u, err := url.Parse(returnTo)
	if err != nil || u.User != nil || !a.returnOrigins[strings.ToLower(u.Scheme+"://"+u.Host)] {
		return &murphy.FieldError{Field: "return_to", Reason: "must be on an origin this install allows"}
	}
	return nil
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
