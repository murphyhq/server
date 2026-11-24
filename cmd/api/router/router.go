package router

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"time"

	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/idempotency"
	"github.com/murphyhq/server/services/catalog"
	"github.com/murphyhq/server/services/escalation"
	"github.com/murphyhq/server/services/incidents"
	"github.com/murphyhq/server/services/oncall"
	"github.com/murphyhq/server/services/people"
)

type Server struct {
	catalog       *catalog.Service
	incidents     *incidents.Service
	people        *people.Service
	oncall        *oncall.Service
	escalation    *escalation.Service
	publicKeys    []ed25519.PublicKey
	auth          *people.Auth
	secureCookies bool
}

type Services struct {
	Catalog       *catalog.Service
	Incidents     *incidents.Service
	People        *people.Service
	OnCall        *oncall.Service
	Escalation    *escalation.Service
	PublicKeys    []ed25519.PublicKey
	Auth          *people.Auth
	SecureCookies bool
}

func New(services Services, keys idempotency.Store) (http.Handler, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("load OpenAPI spec: %w", err)
	}
	rules, err := newAccess(spec, services.People, services.Escalation, services.Catalog, services.OnCall, services.Incidents)
	if err != nil {
		return nil, fmt.Errorf("read access rules from the OpenAPI spec: %w", err)
	}
	strict := NewStrictHandlerWithOptions(&Server{catalog: services.Catalog, incidents: services.Incidents, people: services.People, oncall: services.OnCall, escalation: services.Escalation, publicKeys: services.PublicKeys, auth: services.Auth, secureCookies: services.SecureCookies}, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  httpserver.BadRequest,
		ResponseErrorHandlerFunc: httpserver.WriteError,
	})
	return HandlerWithOptions(strict, StdHTTPServerOptions{
		Middlewares: []MiddlewareFunc{
			idempotency.Middleware(keys, 24*time.Hour),
			httpserver.RejectUnknownParameters(spec),
			requireAccess(rules),
			RequireSession(services.Auth),
		},
		ErrorHandlerFunc: httpserver.BadRequest,
	}), nil
}
