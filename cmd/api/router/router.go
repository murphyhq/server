package router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/idempotency"
	"github.com/murphyhq/server/services/catalog"
)

type Server struct {
	catalog *catalog.Service
}

func New(catalog *catalog.Service, keys idempotency.Store) (http.Handler, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI spec: %w", err)
	}
	strict := NewStrictHandlerWithOptions(&Server{catalog: catalog}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  httpserver.BadRequest,
		ResponseErrorHandlerFunc: httpserver.WriteError,
	})
	return HandlerWithOptions(strict, StdHTTPServerOptions{
		Middlewares: []MiddlewareFunc{
			idempotency.Middleware(keys, 24*time.Hour),
			httpserver.RejectUnknownParameters(spec),
		},
		ErrorHandlerFunc: httpserver.BadRequest,
	}), nil
}
