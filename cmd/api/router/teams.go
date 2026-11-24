package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) ListTeams(ctx context.Context, request ListTeamsRequestObject) (_ ListTeamsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	teams, next, err := s.people.ListTeams(ctx, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Team, len(teams))
	for i, team := range teams {
		items[i] = toTeam(team)
	}
	return ListTeams200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) CreateTeam(ctx context.Context, request CreateTeamRequestObject) (_ CreateTeamResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	team, err := s.people.CreateTeam(ctx, &murphy.Team{Name: request.Body.Name})
	if err != nil {
		return nil, err
	}
	return CreateTeam201JSONResponse(toTeam(team)), nil
}

func (s *Server) GetTeam(ctx context.Context, request GetTeamRequestObject) (_ GetTeamResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	team, err := s.people.GetTeam(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetTeam200JSONResponse(toTeam(team)), nil
}

func (s *Server) UpdateTeam(ctx context.Context, request UpdateTeamRequestObject) (_ UpdateTeamResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	team, err := s.people.UpdateTeam(ctx, &murphy.Team{Id: request.Id, Name: request.Body.Name})
	if err != nil {
		return nil, err
	}
	return UpdateTeam200JSONResponse(toTeam(team)), nil
}

func (s *Server) DeleteTeam(ctx context.Context, request DeleteTeamRequestObject) (_ DeleteTeamResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.people.DeleteTeam(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteTeam204Response{}, nil
}

func (s *Server) ListTeamMembers(ctx context.Context, request ListTeamMembersRequestObject) (_ ListTeamMembersResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	team, memberships, err := s.people.ListMemberships(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	n.knowTeam(team)
	items := make([]TeamMembership, len(memberships))
	for i, membership := range memberships {
		items[i] = toMembership(membership, n)
	}
	return ListTeamMembers200JSONResponse{Items: items}, s.name(ctx, n)
}

func (s *Server) SetTeamMember(ctx context.Context, request SetTeamMemberRequestObject) (_ SetTeamMemberResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	membership, err := s.people.SetMembership(ctx, &murphy.TeamMembership{
		TeamId: request.Id,
		UserId: request.UserId,
		Role:   murphy.TeamRoleFromText(string(request.Body.Role)),
	})
	if err != nil {
		return nil, err
	}
	return SetTeamMember200JSONResponse(toMembership(membership, nil)), nil
}

func (s *Server) RemoveTeamMember(ctx context.Context, request RemoveTeamMemberRequestObject) (_ RemoveTeamMemberResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.people.RemoveMembership(ctx, request.Id, request.UserId); err != nil {
		return nil, err
	}
	return RemoveTeamMember204Response{}, nil
}

func toTeam(team *murphy.Team) Team {
	return Team{Id: team.GetId(), Name: team.GetName(), CreatedAt: team.GetCreatedAt().AsTime(), UpdatedAt: team.GetUpdatedAt().AsTime()}
}

func toMembership(membership *murphy.TeamMembership, n *names) TeamMembership {
	return TeamMembership{
		Team:      n.team(membership.GetTeamId()),
		User:      n.user(membership.GetUserId()),
		Role:      TeamRole(membership.GetRole().Text()),
		CreatedAt: membership.GetCreatedAt().AsTime(),
		UpdatedAt: membership.GetUpdatedAt().AsTime(),
	}
}

func (s *Server) ListUserMemberships(ctx context.Context, request ListUserMembershipsRequestObject) (_ ListUserMembershipsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	user, memberships, err := s.people.ListMembershipsOfUser(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	n := newNames()
	n.knowUser(user)
	items := make([]TeamMembership, len(memberships))
	for i, membership := range memberships {
		items[i] = toMembership(membership, n)
	}
	return ListUserMemberships200JSONResponse{Items: items}, s.name(ctx, n)
}
