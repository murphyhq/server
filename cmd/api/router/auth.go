package router

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpserver"
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
}

func RequireSession(auth *people.Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(SessionScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}
			token := sessionToken(r)
			if token == "" {
				httpserver.WriteError(w, r, murphy.ErrUnauthenticated)
				return
			}
			session, err := auth.Authenticate(r.Context(), token)
			if err != nil {
				httpserver.WriteError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), sessionContext{}, &currentSession{token: token, session: session})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func sessionToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return token
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		return cookie.Value
	}
	return ""
}

func sessionFrom(ctx context.Context) *currentSession {
	current, _ := ctx.Value(sessionContext{}).(*currentSession)
	return current
}

func (s *Server) SignIn(ctx context.Context, _ SignInRequestObject) (_ SignInResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	started, err := s.auth.StartSignIn(ctx)
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
	session, err := s.session(ctx, signedIn.Session)
	if err != nil {
		return nil, err
	}
	cookie := s.cookie(signedIn.Token, signedIn.Session.GetExpiresAt().AsTime())
	return FinishSignIn200JSONResponse{Body: session, Headers: FinishSignIn200ResponseHeaders{SetCookie: &cookie}}, nil
}

func (s *Server) CreateSession(ctx context.Context, request CreateSessionRequestObject) (_ CreateSessionResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	signedIn, err := s.auth.SignInWithIDToken(ctx, request.Body.IdToken)
	if err != nil {
		return nil, err
	}
	session, err := s.session(ctx, signedIn.Session)
	if err != nil {
		return nil, err
	}
	return CreateSession201JSONResponse{Token: signedIn.Token, Session: session}, nil
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
	user, err := s.people.GetUser(ctx, session.GetUserId())
	if err != nil {
		return Session{}, err
	}
	return Session{User: toUser(user), ExpiresAt: session.GetExpiresAt().AsTime()}, nil
}

func (s *Server) cookie(token string, expires time.Time) string {
	cookie := &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
	}
	return cookie.String()
}
