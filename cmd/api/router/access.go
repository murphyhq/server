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
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/telemetry"
)

type TeamRoles interface {
	TeamRolesOf(ctx context.Context, userID string) (map[string]murphy.TeamRole, error)
	GetContactMethod(ctx context.Context, id string) (*murphy.ContactMethod, error)
}

type PolicyOwners interface {
	GetPolicyRecord(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
}

type ServiceOwners interface {
	GetService(ctx context.Context, id string) (*murphy.Service, error)
	GetIntegrationAndService(ctx context.Context, integrationID string) (*murphy.Integration, *murphy.Service, error)
}

type ScheduleOwners interface {
	TeamsOfSchedule(ctx context.Context, id string) ([]string, error)
	GetOverride(ctx context.Context, id string) (*murphy.Override, error)
}

type IncidentResponders interface {
	PolicyOfIncident(ctx context.Context, id, userID string) (policyID string, paged bool, err error)
}

type rule struct {
	adminOnly bool
	anyone    bool
	evenAdmin bool
	check     func(a *access, ctx context.Context, c caller, id string) error
	owner     func(a *access, ctx context.Context, id string) ([]string, error)
	covers    bool
}

var rules = map[string]rule{
	"sessions:own":        {anyone: true},
	"install:manage":      {adminOnly: true},
	"users:self":          {check: (*access).isSelf},
	"users:only-self":     {check: (*access).isOnlySelf, evenAdmin: true},
	"contact-methods:own": {check: (*access).ownsContactMethod},
	"incidents:respond":   {check: (*access).respondsTo},
	"teams:manage":        {owner: (*access).theTeam},
	"policies:manage":     {owner: (*access).teamOfPolicy},
	"services:manage":     {owner: (*access).teamOfService},
	"integrations:manage": {owner: (*access).teamOfIntegration},
	"schedules:manage":    {owner: (*access).teamsOfSchedule},
	"overrides:create":    {owner: (*access).teamsOfSchedule, covers: true},
	"overrides:manage":    {owner: (*access).teamsOfOverride, covers: true},
}

type grant struct {
	scope    string
	resource resource
}

type access struct {
	grants    map[string][]grant
	people    TeamRoles
	policies  PolicyOwners
	services  ServiceOwners
	schedules ScheduleOwners
	incidents IncidentResponders
}

type caller struct {
	id    string
	roles func() (map[string]murphy.TeamRole, error)
}

func (a *access) caller(ctx context.Context, userID string) caller {
	return caller{id: userID, roles: sync.OnceValues(func() (map[string]murphy.TeamRole, error) {
		return a.people.TeamRolesOf(ctx, userID)
	})}
}

func requireAccess(a *access) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := sessionFrom(r.Context())
			grants := a.grants[r.Pattern]
			if current != nil && current.role == murphy.UserRole_USER_ROLE_ADMIN {
				grants = slices.DeleteFunc(slices.Clone(grants), func(g grant) bool { return !rules[g.scope].evenAdmin })
			}
			if current == nil || len(grants) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			body, err := jsonBody(r)
			if err != nil {
				httpserver.WriteProblem(w, r, http.StatusBadRequest, "the request body is not valid JSON")
				return
			}
			c := a.caller(r.Context(), current.session.GetUserId())
			for _, g := range grants {
				ids, applies := g.resource.in(r, body)
				if !applies {
					continue
				}
				if err := a.allows(r.Context(), c, g.scope, ids); err != nil {
					httpserver.WriteError(w, r, err)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a *access) allows(ctx context.Context, c caller, scope string, ids []string) error {
	rule := rules[scope]
	switch {
	case rule.anyone:
		return nil
	case rule.adminOnly:
		return &murphy.ForbiddenError{Reason: "only admins can do this"}
	case rule.check != nil:
		return rule.check(a, ctx, c, ids[0])
	}
	var teams []string
	for _, id := range ids {
		owners, err := rule.owner(a, ctx, id)
		if err != nil {
			return err
		}
		teams = append(teams, owners...)
	}
	if rule.covers {
		return hasRoleIn(c, teams, "only the team's managers and responders can do this",
			murphy.TeamRole_TEAM_ROLE_MANAGER, murphy.TeamRole_TEAM_ROLE_RESPONDER)
	}
	return hasRoleIn(c, teams, "only the team's managers can do this", murphy.TeamRole_TEAM_ROLE_MANAGER)
}

func (a *access) isSelf(_ context.Context, c caller, id string) error {
	if id == c.id {
		return nil
	}
	return &murphy.ForbiddenError{Reason: "only admins can do this for someone else"}
}

func (a *access) isOnlySelf(_ context.Context, c caller, id string) error {
	if id == c.id {
		return nil
	}
	return &murphy.ForbiddenError{Reason: "only the person themselves can do this"}
}

func (a *access) ownsContactMethod(ctx context.Context, c caller, id string) error {
	method, err := a.people.GetContactMethod(ctx, id)
	if err != nil {
		return err
	}
	return a.isSelf(ctx, c, method.GetUserId())
}

func (a *access) respondsTo(ctx context.Context, c caller, id string) error {
	telemetry.Collect(ctx, telemetry.WithField("incident.id", id))
	policyID, paged, err := a.incidents.PolicyOfIncident(ctx, id, c.id)
	if err != nil {
		return err
	}
	if paged {
		return nil
	}
	teams, err := a.teamOfPolicy(ctx, policyID)
	if err != nil {
		return err
	}
	return mayRespond(c, teams)
}

func mayRespond(c caller, teams []string) error {
	return hasRoleIn(c, teams, "only the owning team's responders and managers, and people paged for it, can respond to this incident",
		murphy.TeamRole_TEAM_ROLE_RESPONDER, murphy.TeamRole_TEAM_ROLE_MANAGER)
}

func hasRoleIn(c caller, teams []string, refusal string, allowed ...murphy.TeamRole) error {
	roles, err := c.roles()
	if err != nil {
		return fmt.Errorf("check team roles: %w", err)
	}
	if slices.ContainsFunc(teams, func(team string) bool { return slices.Contains(allowed, roles[team]) }) {
		return nil
	}
	return &murphy.ForbiddenError{Reason: refusal}
}

func (a *access) theTeam(_ context.Context, id string) ([]string, error) {
	return []string{id}, nil
}

func (a *access) teamOfPolicy(ctx context.Context, id string) ([]string, error) {
	policy, err := a.policies.GetPolicyRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	return []string{policy.GetTeamId()}, nil
}

func (a *access) teamOfService(ctx context.Context, id string) ([]string, error) {
	service, err := a.services.GetService(ctx, id)
	if err != nil {
		return nil, err
	}
	return a.teamOfPolicy(ctx, service.GetEscalationPolicyId())
}

func (a *access) teamOfIntegration(ctx context.Context, id string) ([]string, error) {
	_, service, err := a.services.GetIntegrationAndService(ctx, id)
	if err != nil {
		return nil, err
	}
	return a.teamOfPolicy(ctx, service.GetEscalationPolicyId())
}

func (a *access) teamsOfSchedule(ctx context.Context, id string) ([]string, error) {
	return a.schedules.TeamsOfSchedule(ctx, id)
}

func (a *access) teamsOfOverride(ctx context.Context, id string) ([]string, error) {
	override, err := a.schedules.GetOverride(ctx, id)
	if err != nil {
		return nil, err
	}
	return a.teamsOfSchedule(ctx, override.GetScheduleId())
}

type resource struct {
	inPath   string
	inBody   []string
	optional bool
}

func (where resource) in(r *http.Request, body any) ([]string, bool) {
	switch {
	case where.inPath != "":
		return []string{r.PathValue(where.inPath)}, true
	case where.inBody != nil:
		ids := lookup(body, where.inBody)
		return ids, len(ids) > 0 || !where.optional
	default:
		return nil, true
	}
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
	scopes := requirements[0]["session"]
	if method != http.MethodGet && len(scopes) == 0 {
		return nil, fmt.Errorf("a change must declare a scope")
	}
	declared, _ := operation.Extensions["x-murphy-resources"].(map[string]any)
	grants := make([]grant, len(scopes))
	for i, scope := range scopes {
		rule, known := rules[scope]
		if !known {
			return nil, fmt.Errorf("unknown scope %q", scope)
		}
		where, err := resourceOf(path, rule, declared[scope])
		if err != nil {
			return nil, fmt.Errorf("scope %q: %w", scope, err)
		}
		grants[i] = grant{scope: scope, resource: where}
	}
	return grants, nil
}

func resourceOf(path string, rule rule, declared any) (resource, error) {
	if text, ok := declared.(string); ok {
		field, optional := strings.CutSuffix(strings.TrimPrefix(text, "body."), "?")
		return resource{inBody: strings.Split(field, "."), optional: optional}, nil
	}
	if rule.anyone || rule.adminOnly {
		return resource{}, nil
	}
	if !strings.Contains(path, "{id}") {
		return resource{}, fmt.Errorf("needs a resource: an {id} in the path, or where to find it in x-murphy-resources")
	}
	return resource{inPath: "id"}, nil
}
