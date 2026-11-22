package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpclient"
	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/services/catalog"
)

const basicUsername = "murphy"

type Server struct {
	publisher *messaging.Publisher
	client    *http.Client
}

func New(publisher *messaging.Publisher, keys *catalog.Keyring) (http.Handler, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI spec: %w", err)
	}
	strict := NewStrictHandlerWithOptions(&Server{publisher: publisher, client: httpclient.New(10 * time.Second)}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  httpserver.BadRequest,
		ResponseErrorHandlerFunc: httpserver.WriteError,
	})
	return HandlerWithOptions(strict, StdHTTPServerOptions{
		Middlewares: []MiddlewareFunc{
			httpserver.RejectUnknownParameters(spec),
			authenticate(keys),
		},
		ErrorHandlerFunc: httpserver.BadRequest,
	}), nil
}

type keyContext struct{}

func authenticate(keys *catalog.Keyring) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := keys.Verify(credential(r))
			if err != nil {
				w.Header().Add("WWW-Authenticate", `Basic realm="murphy"`)
				w.Header().Add("WWW-Authenticate", `Bearer realm="murphy"`)
				httpserver.WriteProblem(w, r, http.StatusUnauthorized, "invalid integration key")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyContext{}, claims)))
		})
	}
}

func credential(r *http.Request) string {
	if username, password, ok := r.BasicAuth(); ok {
		if username != basicUsername {
			return ""
		}
		return password
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return token
}

func keyFrom(ctx context.Context) *murphy.IntegrationKey {
	claims, _ := ctx.Value(keyContext{}).(*murphy.IntegrationKey)
	return claims
}
