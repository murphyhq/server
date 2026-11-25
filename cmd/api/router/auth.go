package router

import (
	"context"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/people"
)

const (
	sessionCookie = "murphy_session"
	signInCookie  = "murphy_sign_in"
)

type sessionContext struct{}

type currentSession struct {
	token   string
	session *murphy.Session
	role    murphy.UserRole
}

func RequireSession(auth *people.Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(SessionScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}
			token, fromCookie := sessionToken(r)
			if token == "" {
				httpserver.WriteError(w, r, murphy.ErrUnauthenticated)
				return
			}
			if fromCookie && !jsonOrEmpty(r) {
				httpserver.WriteProblem(w, r, http.StatusUnsupportedMediaType, "requests signed in with the session cookie must send JSON")
				return
			}
			session, role, err := auth.Authenticate(r.Context(), token)
			if err != nil {
				httpserver.WriteError(w, r, err)
				return
			}
			who := []telemetry.Field{telemetry.WithField("user.id", session.GetUserId()), telemetry.WithField("session.id", session.GetId())}
			telemetry.Collect(r.Context(), who...)
			ctx := context.WithValue(telemetry.With(r.Context(), who...), sessionContext{}, &currentSession{token: token, session: session, role: role})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func sessionToken(r *http.Request) (string, bool) {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return token, false
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		return cookie.Value, true
	}
	return "", false
}

func jsonOrEmpty(r *http.Request) bool {
	if r.ContentLength == 0 || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func sessionFrom(ctx context.Context) *currentSession {
	current, _ := ctx.Value(sessionContext{}).(*currentSession)
	return current
}

func (s *Server) SignIn(ctx context.Context, request SignInRequestObject) (_ SignInResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	started, err := s.auth.StartSignIn(ctx, *murphy.Or(request.Params.ReturnTo, new(string)), *murphy.Or(request.Params.CodeChallenge, new(string)))
	if err != nil {
		return nil, err
	}
	cookie := (&http.Cookie{
		Name: signInCookie, Value: started.State, Path: "/v1/auth/callback", MaxAge: 600,
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
	}).String()
	return SignIn302Response{Headers: SignIn302ResponseHeaders{Location: &started.URL, SetCookie: &cookie}}, nil
}

func (s *Server) FinishSignIn(ctx context.Context, request FinishSignInRequestObject) (_ FinishSignInResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if request.Params.Error != nil || request.Params.Code == nil || request.Params.State == nil || request.Params.MurphySignIn == nil {
		return nil, murphy.ErrUnauthenticated
	}
	signedIn, err := s.auth.FinishSignIn(ctx, *request.Params.State, *request.Params.MurphySignIn, *request.Params.Code)
	if err != nil {
		return nil, err
	}
	if signedIn.Session == nil {
		return FinishSignIn303Response{Headers: FinishSignIn303ResponseHeaders{Location: &signedIn.ReturnTo}}, nil
	}
	cookie := s.cookie(signedIn.Token, signedIn.Session.GetRenewableUntil().AsTime())
	if signedIn.ReturnTo != "" {
		return FinishSignIn303Response{Headers: FinishSignIn303ResponseHeaders{Location: &signedIn.ReturnTo, SetCookie: &cookie}}, nil
	}
	session, err := s.session(ctx, signedIn.Session)
	if err != nil {
		return nil, err
	}
	return FinishSignIn200JSONResponse{Body: session, Headers: FinishSignIn200ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) CreateSession(ctx context.Context, request CreateSessionRequestObject) (_ CreateSessionResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	signedIn, err := s.signInWith(ctx, request.Body)
	if err != nil {
		return nil, err
	}
	session, err := s.session(ctx, signedIn.Session)
	if err != nil {
		return nil, err
	}
	return CreateSession201JSONResponse{Token: signedIn.Token, Session: session}, nil
}

func (s *Server) signInWith(ctx context.Context, body *CreateSessionJSONRequestBody) (*people.SignedIn, error) {
	ways := 0
	for _, sent := range []bool{body.IdToken != nil, body.Code != nil, body.Email != nil || body.Password != nil} {
		if sent {
			ways++
		}
	}
	switch {
	case ways != 1:
		return nil, &murphy.FieldError{Field: "id_token", Reason: "code, or email and password must be sent, and only one of them"}
	case body.IdToken != nil:
		return s.auth.SignInWithIDToken(ctx, *body.IdToken)
	case body.Code != nil:
		return s.auth.SignInWithCode(ctx, *body.Code, *murphy.Or(body.CodeVerifier, new(string)))
	default:
		return s.auth.SignInWithPassword(ctx, *murphy.Or(body.Email, new(string)), *murphy.Or(body.Password, new(string)), httpserver.ClientAddress(ctx))
	}
}

func (s *Server) CreateConnectCode(ctx context.Context, _ CreateConnectCodeRequestObject) (_ CreateConnectCodeResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	connect, err := s.auth.CreateConnectCode(ctx, sessionFrom(ctx).session.GetUserId())
	if err != nil {
		return nil, err
	}
	return CreateConnectCode201JSONResponse{Code: connect.Code, Link: connect.Link, ExpiresAt: connect.ExpiresAt}, nil
}

func (s *Server) ListSessions(ctx context.Context, request ListSessionsRequestObject) (_ ListSessionsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	current := sessionFrom(ctx).session
	sessions, next, err := s.auth.ListSessions(ctx, current.GetUserId(), page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]SessionSummary, len(sessions))
	for i, session := range sessions {
		items[i] = SessionSummary{
			Id: session.GetId(), Client: SessionSummaryClient(session.GetClient().Text()), Current: session.GetId() == current.GetId(),
			CreatedAt: session.GetCreatedAt().AsTime(), ExpiresAt: session.GetExpiresAt().AsTime(),
		}
	}
	return ListSessions200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) EndSession(ctx context.Context, request EndSessionRequestObject) (_ EndSessionResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.auth.EndSession(ctx, sessionFrom(ctx).session.GetUserId(), request.Id); err != nil {
		return nil, err
	}
	return EndSession204Response{}, nil
}

func (s *Server) SignOutEverywhere(ctx context.Context, _ SignOutEverywhereRequestObject) (_ SignOutEverywhereResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.auth.EndAllSessions(ctx, sessionFrom(ctx).session.GetUserId()); err != nil {
		return nil, err
	}
	cookie := s.cookie("", time.Unix(0, 0))
	return SignOutEverywhere204Response{Headers: SignOutEverywhere204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) EndUserSessions(ctx context.Context, request EndUserSessionsRequestObject) (_ EndUserSessionsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	user, err := s.people.GetUser(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	if err := s.auth.EndAllSessions(ctx, user.GetId()); err != nil {
		return nil, err
	}
	return EndUserSessions204Response{}, nil
}

func (s *Server) GetSession(ctx context.Context, _ GetSessionRequestObject) (_ GetSessionResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	session, err := s.session(ctx, sessionFrom(ctx).session)
	if err != nil {
		return nil, err
	}
	return GetSession200JSONResponse(session), nil
}

func (s *Server) SignOut(ctx context.Context, _ SignOutRequestObject) (_ SignOutResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.auth.SignOut(ctx, sessionFrom(ctx).token); err != nil {
		return nil, err
	}
	cookie := s.cookie("", time.Unix(0, 0))
	return SignOut204Response{Headers: SignOut204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) session(ctx context.Context, session *murphy.Session) (Session, error) {
	user, memberships, err := s.people.ListMembershipsOfUser(ctx, session.GetUserId())
	if err != nil {
		return Session{}, err
	}
	n := newNames()
	n.knowUser(user)
	out := Session{User: toUser(user), Memberships: make([]TeamMembership, len(memberships)), ExpiresAt: session.GetExpiresAt().AsTime()}
	for i, membership := range memberships {
		out.Memberships[i] = toMembership(membership, n)
	}
	return out, s.name(ctx, n)
}

func (s *Server) cookie(token string, expires time.Time) string {
	cookie := &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}

func (s *Server) CreatePasswordLink(ctx context.Context, request CreatePasswordLinkRequestObject) (_ CreatePasswordLinkResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	link, err := s.auth.CreatePasswordLink(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return CreatePasswordLink201JSONResponse{Link: link.Link, ExpiresAt: link.ExpiresAt}, nil
}

func (s *Server) SetPassword(ctx context.Context, request SetPasswordRequestObject) (_ SetPasswordResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.auth.SetPassword(ctx, request.Body.Code, request.Body.Password); err != nil {
		return nil, err
	}
	return SetPassword204Response{}, nil
}
