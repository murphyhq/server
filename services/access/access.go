package access

import (
	"context"
	"fmt"
	"slices"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

type Caller struct {
	UserID string
	Role   murphy.UserRole
}

func (c Caller) admin() bool {
	return c.Role == murphy.UserRole_USER_ROLE_ADMIN
}

type People interface {
	TeamRolesOf(ctx context.Context, userID string) (map[string]murphy.TeamRole, error)
}

type Policies interface {
	GetPolicy(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
}

type Catalog interface {
	GetService(ctx context.Context, id string) (*murphy.Service, error)
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
}

type Schedules interface {
	GetSchedule(ctx context.Context, id string) (*murphy.Schedule, error)
	GetOverride(ctx context.Context, id string) (*murphy.Override, error)
}

type Incidents interface {
	GetIncident(ctx context.Context, id string) (*murphy.Incident, []*murphy.TimelineEntry, error)
}

type Access struct {
	people    People
	policies  Policies
	catalog   Catalog
	schedules Schedules
	incidents Incidents
}

func New(people People, policies Policies, catalog Catalog, schedules Schedules, incidents Incidents) *Access {
	return &Access{people: people, policies: policies, catalog: catalog, schedules: schedules, incidents: incidents}
}

func (a *Access) Admin(caller Caller) error {
	if caller.admin() {
		return nil
	}
	return &murphy.ForbiddenError{Reason: "only admins can do this"}
}

func (a *Access) SelfOrAdmin(caller Caller, userID string) error {
	if caller.admin() || caller.UserID == userID {
		return nil
	}
	return &murphy.ForbiddenError{Reason: "only admins can do this for someone else"}
}

func (a *Access) ManagesTeams(ctx context.Context, caller Caller, teamIDs ...string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	roles, err := a.people.TeamRolesOf(ctx, caller.UserID)
	if err != nil {
		return fmt.Errorf("check team roles: %w", err)
	}
	if slices.ContainsFunc(teamIDs, func(team string) bool { return roles[team] == murphy.TeamRole_TEAM_ROLE_MANAGER }) {
		return nil
	}
	return &murphy.ForbiddenError{Reason: "only the team's managers can do this"}
}

func (a *Access) ManagesPolicy(ctx context.Context, caller Caller, policyID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	policy, err := a.policies.GetPolicy(ctx, policyID)
	if err != nil {
		return err
	}
	return a.ManagesTeams(ctx, caller, policy.GetTeamId())
}

func (a *Access) ManagesService(ctx context.Context, caller Caller, serviceID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	service, err := a.catalog.GetService(ctx, serviceID)
	if err != nil {
		return err
	}
	return a.ManagesPolicy(ctx, caller, service.GetEscalationPolicyId())
}

func (a *Access) ManagesIntegration(ctx context.Context, caller Caller, integrationID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	integration, err := a.catalog.GetIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	return a.ManagesService(ctx, caller, integration.GetServiceId())
}

func (a *Access) ManagesSchedule(ctx context.Context, caller Caller, scheduleID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	schedule, err := a.schedules.GetSchedule(ctx, scheduleID)
	if err != nil {
		return err
	}
	return a.ManagesTeams(ctx, caller, schedule.GetTeamIds()...)
}

func (a *Access) ManagesOverride(ctx context.Context, caller Caller, overrideID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	override, err := a.schedules.GetOverride(ctx, overrideID)
	if err != nil {
		return err
	}
	return a.ManagesSchedule(ctx, caller, override.GetScheduleId())
}

func (a *Access) Responds(ctx context.Context, caller Caller, incidentID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if caller.admin() {
		return nil
	}
	incident, timeline, err := a.incidents.GetIncident(ctx, incidentID)
	if err != nil {
		return err
	}
	if paged(timeline, caller.UserID) {
		return nil
	}
	policy, err := a.policies.GetPolicy(ctx, incident.GetEscalationPolicyId())
	if err != nil {
		return err
	}
	roles, err := a.people.TeamRolesOf(ctx, caller.UserID)
	if err != nil {
		return fmt.Errorf("check team roles: %w", err)
	}
	switch roles[policy.GetTeamId()] {
	case murphy.TeamRole_TEAM_ROLE_RESPONDER, murphy.TeamRole_TEAM_ROLE_MANAGER:
		return nil
	default:
		return &murphy.ForbiddenError{Reason: "only the owning team's responders and managers, and people paged for it, can respond to this incident"}
	}
}

func paged(timeline []*murphy.TimelineEntry, userID string) bool {
	for _, entry := range timeline {
		for _, page := range entry.GetEscalation().GetPaged() {
			if page.GetUserId() == userID {
				return true
			}
		}
	}
	return false
}
