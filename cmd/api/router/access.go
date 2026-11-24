package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpserver"
)

type TeamRoles interface {
	TeamRolesOf(ctx context.Context, userID string) (map[string]murphy.TeamRole, error)
	GetContactMethod(ctx context.Context, id string) (*murphy.ContactMethod, error)
}

type PolicyOwners interface {
	GetPolicy(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
}

type ServiceOwners interface {
	GetService(ctx context.Context, id string) (*murphy.Service, error)
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
}

type ScheduleOwners interface {
	GetSchedule(ctx context.Context, id string) (*murphy.Schedule, error)
	GetOverride(ctx context.Context, id string) (*murphy.Override, error)
}

type IncidentResponders interface {
	GetIncident(ctx context.Context, id string) (*murphy.Incident, []*murphy.TimelineEntry, error)
}

type caller struct {
	userID string
	role   murphy.UserRole
}

type grant struct {
	scope    string
	resource locator
}

type access struct {
	grants    map[string][]grant
	people    TeamRoles
	policies  PolicyOwners
	services  ServiceOwners
	schedules ScheduleOwners
	incidents IncidentResponders
}

var scopes = map[string]bool{
	"sessions:own": false, "install:manage": false,
	"users:self": true, "contact-methods:own": true, "incidents:respond": true,
	"teams:manage": true, "policies:manage": true, "services:manage": true,
	"integrations:manage": true, "schedules:manage": true, "overrides:manage": true,
}

func (a *access) teamsOf(scope string) func(ctx context.Context, id string) ([]string, error) {
	return map[string]func(context.Context, string) ([]string, error){
		"teams:manage": func(_ context.Context, id string) ([]string, error) { return []string{id}, nil },
		"policies:manage": func(ctx context.Context, id string) ([]string, error) {
			policy, err := a.policies.GetPolicy(ctx, id)
			return []string{policy.GetTeamId()}, err
		},
		"services:manage": func(ctx context.Context, id string) ([]string, error) {
			service, err := a.services.GetService(ctx, id)
			return chain(ctx, err, a.teamsOf("policies:manage"), service.GetEscalationPolicyId())
		},
		"integrations:manage": func(ctx context.Context, id string) ([]string, error) {
			integration, err := a.services.GetIntegration(ctx, id)
			return chain(ctx, err, a.teamsOf("services:manage"), integration.GetServiceId())
		},
		"schedules:manage": func(ctx context.Context, id string) ([]string, error) {
			schedule, err := a.schedules.GetSchedule(ctx, id)
			return schedule.GetTeamIds(), err
		},
		"overrides:manage": func(ctx context.Context, id string) ([]string, error) {
			override, err := a.schedules.GetOverride(ctx, id)
			return chain(ctx, err, a.teamsOf("schedules:manage"), override.GetScheduleId())
		},
	}[scope]
}

func chain(ctx context.Context, err error, next func(context.Context, string) ([]string, error), id string) ([]string, error) {
	if err != nil {
		return nil, err
	}
	return next(ctx, id)
}

func (a *access) allows(ctx context.Context, who caller, scope string, ids []string) error {
	switch {
	case who.role == murphy.UserRole_USER_ROLE_ADMIN, scope == "sessions:own":
		return nil
	case scope == "install:manage":
		return &murphy.ForbiddenError{Reason: "only admins can do this"}
	case scope == "users:self":
		return a.self(who, ids[0])
	case scope == "contact-methods:own":
		method, err := a.people.GetContactMethod(ctx, ids[0])
		if err != nil {
			return err
		}
		return a.self(who, method.GetUserId())
	case scope == "incidents:respond":
		return a.responds(ctx, who, ids[0])
	}
	var teams []string
	for _, id := range ids {
		owners, err := a.teamsOf(scope)(ctx, id)
		if err != nil {
			return err
		}
		teams = append(teams, owners...)
	}
	return a.hasRole(ctx, who, teams, "only the team's managers can do this", murphy.TeamRole_TEAM_ROLE_MANAGER)
}

func (a *access) self(who caller, userID string) error {
	if userID == who.userID {
		return nil
	}
	return &murphy.ForbiddenError{Reason: "only admins can do this for someone else"}
}

func (a *access) responds(ctx context.Context, who caller, incidentID string) error {
	incident, timeline, err := a.incidents.GetIncident(ctx, incidentID)
	if err != nil {
		return err
	}
	for _, entry := range timeline {
		for _, page := range entry.GetEscalation().GetPaged() {
			if page.GetUserId() == who.userID {
				return nil
			}
		}
	}
	teams, err := a.teamsOf("policies:manage")(ctx, incident.GetEscalationPolicyId())
	if err != nil {
		return err
	}
	return a.hasRole(ctx, who, teams, "only the owning team's responders and managers, and people paged for it, can respond to this incident",
		murphy.TeamRole_TEAM_ROLE_RESPONDER, murphy.TeamRole_TEAM_ROLE_MANAGER)
}

func (a *access) hasRole(ctx context.Context, who caller, teams []string, refusal string, allowed ...murphy.TeamRole) error {
	roles, err := a.people.TeamRolesOf(ctx, who.userID)
	if err != nil {
		return fmt.Errorf("check team roles: %w", err)
	}
	if slices.ContainsFunc(teams, func(team string) bool { return slices.Contains(allowed, roles[team]) }) {
		return nil
	}
	return &murphy.ForbiddenError{Reason: refusal}
}

func requireAccess(a *access) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := sessionFrom(r.Context())
			grants := a.grants[r.Pattern]
			if current == nil || len(grants) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			body, err := jsonBody(r)
			if err != nil {
				httpserver.WriteProblem(w, r, http.StatusBadRequest, "the request body is not valid JSON")
				return
			}
			who := caller{userID: current.session.GetUserId(), role: current.role}
			for _, g := range grants {
				ids, applies := g.resource.find(r, body)
				if !applies {
					continue
				}
				if err := a.allows(r.Context(), who, g.scope, ids); err != nil {
					httpserver.WriteError(w, r, err)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func jsonBody(r *http.Request) (any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var body any
	return body, json.Unmarshal(raw, &body)
}

func newAccess(spec *openapi3.T, people TeamRoles, policies PolicyOwners, services ServiceOwners, schedules ScheduleOwners, incidents IncidentResponders) (*access, error) {
	a := &access{grants: map[string][]grant{}, people: people, policies: policies, services: services, schedules: schedules, incidents: incidents}
	for path, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			grants, err := grantsOf(spec, path, method, operation)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, path, err)
			}
			a.grants[method+" "+path] = grants
		}
	}
	return a, nil
}

func grantsOf(spec *openapi3.T, path, method string, operation *openapi3.Operation) ([]grant, error) {
	requirements := spec.Security
	if operation.Security != nil {
		requirements = *operation.Security
	}
	if len(requirements) == 0 {
		return nil, nil
	}
	declared := requirements[0]["session"]
	if method != http.MethodGet && len(declared) == 0 {
		return nil, fmt.Errorf("a change must declare a scope")
	}
	resources, _ := operation.Extensions["x-murphy-resources"].(map[string]any)
	grants := make([]grant, len(declared))
	for i, scope := range declared {
		needsResource, known := scopes[scope]
		if !known {
			return nil, fmt.Errorf("unknown scope %q", scope)
		}
		where, _ := resources[scope].(string)
		if needsResource && where == "" {
			where = "path.id"
		}
		if where == "path.id" && !strings.Contains(path, "{id}") {
			return nil, fmt.Errorf("scope %q needs a resource: an {id} in the path, or where to find it in x-murphy-resources", scope)
		}
		grants[i] = grant{scope: scope, resource: locator(where)}
	}
	return grants, nil
}

type locator string

func (l locator) find(r *http.Request, body any) ([]string, bool) {
	where, optional := strings.CutSuffix(string(l), "?")
	source, field, _ := strings.Cut(where, ".")
	switch source {
	case "":
		return nil, true
	case "path":
		return []string{r.PathValue(field)}, true
	}
	values := lookup(body, strings.Split(field, "."))
	return values, len(values) > 0 || !optional
}

func lookup(value any, fields []string) []string {
	if items, ok := value.([]any); ok {
		var found []string
		for _, item := range items {
			found = append(found, lookup(item, fields)...)
		}
		return found
	}
	if len(fields) == 0 {
		text, ok := value.(string)
		if !ok {
			return nil
		}
		return []string{text}
	}
	object, _ := value.(map[string]any)
	next, ok := object[fields[0]]
	if !ok {
		return nil
	}
	return lookup(next, fields[1:])
}
