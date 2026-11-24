package people

import (
	"context"
	"fmt"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Repository interface {
	CreateUser(ctx context.Context, user *murphy.User) error
	GetUser(ctx context.Context, id string) (*murphy.User, error)
	ListUsers(ctx context.Context, before string, limit int) ([]*murphy.User, error)
	UpdateUser(ctx context.Context, user *murphy.User) error
	DeleteUser(ctx context.Context, id string) error
	CreateContactMethod(ctx context.Context, method *murphy.ContactMethod) error
	GetContactMethod(ctx context.Context, id string) (*murphy.ContactMethod, error)
	ListContactMethods(ctx context.Context, userID string) ([]*murphy.ContactMethod, error)
	ListContactMethodsOfUsers(ctx context.Context, userIDs []string) ([]*murphy.ContactMethod, error)
	UpdateContactMethod(ctx context.Context, method *murphy.ContactMethod) error
	DeleteContactMethod(ctx context.Context, id string) error
	CreateTeam(ctx context.Context, team *murphy.Team) error
	GetTeam(ctx context.Context, id string) (*murphy.Team, error)
	ListTeams(ctx context.Context, before string, limit int) ([]*murphy.Team, error)
	UpdateTeam(ctx context.Context, team *murphy.Team) error
	DeleteTeam(ctx context.Context, id string) error
	SetMembership(ctx context.Context, membership *murphy.TeamMembership) error
	GetMembership(ctx context.Context, teamID, userID string) (*murphy.TeamMembership, error)
	ListMemberships(ctx context.Context, teamID string) ([]*murphy.TeamMembership, error)
	DeleteMembership(ctx context.Context, teamID, userID string) error
	ExistingUsers(ctx context.Context, ids []string) ([]string, error)
	ExistingTeams(ctx context.Context, ids []string) ([]string, error)
	ActiveUsers(ctx context.Context, ids []string) ([]string, error)
}

type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateUser(ctx context.Context, in *murphy.User) (_ *murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := murphy.Now()
	user := &murphy.User{
		Id:        murphy.NewID(),
		Name:      in.GetName(),
		Email:     in.GetEmail(),
		TimeZone:  murphy.Or(in.GetTimeZone(), "UTC"),
		Status:    murphy.Or(in.GetStatus(), murphy.UserStatus_USER_STATUS_ACTIVE),
		CreatedAt: now,
		UpdatedAt: now,
	}
	ctx = span.Set(ctx, telemetry.WithField("user.id", user.GetId()))
	if err := user.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *Service) GetUser(ctx context.Context, id string) (_ *murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("user", id); err != nil {
		return nil, err
	}
	user, err := s.repo.GetUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

func (s *Service) ListUsers(ctx context.Context, page pagination.Page) (_ []*murphy.User, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	users, err := s.repo.ListUsers(ctx, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list users: %w", err)
	}
	items, next := pagination.Trim(users, window)
	return items, next, nil
}

func (s *Service) UpdateUser(ctx context.Context, in *murphy.User) (_ *murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", in.GetId()))
	defer span.End(&err)

	current, err := s.GetUser(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	user := &murphy.User{
		Id:        current.GetId(),
		Name:      in.GetName(),
		Email:     in.GetEmail(),
		TimeZone:  murphy.Or(in.GetTimeZone(), current.GetTimeZone()),
		Status:    murphy.Or(in.GetStatus(), current.GetStatus()),
		CreatedAt: current.GetCreatedAt(),
		UpdatedAt: murphy.Now(),
	}
	if err := user.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return user, nil
}

func (s *Service) DeleteUser(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("user", id); err != nil {
		return err
	}
	if err := s.repo.DeleteUser(ctx, id); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func (s *Service) CreateContactMethod(ctx context.Context, in *murphy.ContactMethod) (_ *murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", in.GetUserId()))
	defer span.End(&err)

	now := murphy.Now()
	method := &murphy.ContactMethod{
		Id:        murphy.NewID(),
		UserId:    in.GetUserId(),
		Type:      in.GetType(),
		Address:   in.GetAddress(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := method.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.GetUser(ctx, method.GetUserId()); err != nil {
		return nil, err
	}
	if err := s.repo.CreateContactMethod(ctx, method); err != nil {
		return nil, fmt.Errorf("create contact method: %w", err)
	}
	return method, nil
}

func (s *Service) GetContactMethod(ctx context.Context, id string) (_ *murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("contact_method.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("contact method", id); err != nil {
		return nil, err
	}
	method, err := s.repo.GetContactMethod(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get contact method: %w", err)
	}
	return method, nil
}

func (s *Service) ListContactMethods(ctx context.Context, userID string) (_ []*murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	if _, err := s.GetUser(ctx, userID); err != nil {
		return nil, err
	}
	methods, err := s.repo.ListContactMethods(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list contact methods: %w", err)
	}
	return methods, nil
}

func (s *Service) UpdateContactMethod(ctx context.Context, in *murphy.ContactMethod) (_ *murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("contact_method.id", in.GetId()))
	defer span.End(&err)

	current, err := s.GetContactMethod(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	method := &murphy.ContactMethod{
		Id:        current.GetId(),
		UserId:    current.GetUserId(),
		Type:      murphy.Or(in.GetType(), current.GetType()),
		Address:   in.GetAddress(),
		CreatedAt: current.GetCreatedAt(),
		UpdatedAt: murphy.Now(),
	}
	if err := method.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateContactMethod(ctx, method); err != nil {
		return nil, fmt.Errorf("update contact method: %w", err)
	}
	return method, nil
}

func (s *Service) DeleteContactMethod(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("contact_method.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("contact method", id); err != nil {
		return err
	}
	if err := s.repo.DeleteContactMethod(ctx, id); err != nil {
		return fmt.Errorf("delete contact method: %w", err)
	}
	return nil
}

func (s *Service) CreateTeam(ctx context.Context, in *murphy.Team) (_ *murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := murphy.Now()
	team := &murphy.Team{Id: murphy.NewID(), Name: in.GetName(), CreatedAt: now, UpdatedAt: now}
	ctx = span.Set(ctx, telemetry.WithField("team.id", team.GetId()))
	if err := team.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.CreateTeam(ctx, team); err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	return team, nil
}

func (s *Service) GetTeam(ctx context.Context, id string) (_ *murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("team", id); err != nil {
		return nil, err
	}
	team, err := s.repo.GetTeam(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get team: %w", err)
	}
	return team, nil
}

func (s *Service) ListTeams(ctx context.Context, page pagination.Page) (_ []*murphy.Team, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	teams, err := s.repo.ListTeams(ctx, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list teams: %w", err)
	}
	items, next := pagination.Trim(teams, window)
	return items, next, nil
}

func (s *Service) UpdateTeam(ctx context.Context, in *murphy.Team) (_ *murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", in.GetId()))
	defer span.End(&err)

	current, err := s.GetTeam(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	team := &murphy.Team{Id: current.GetId(), Name: in.GetName(), CreatedAt: current.GetCreatedAt(), UpdatedAt: murphy.Now()}
	if err := team.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateTeam(ctx, team); err != nil {
		return nil, fmt.Errorf("update team: %w", err)
	}
	return team, nil
}

func (s *Service) DeleteTeam(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("team", id); err != nil {
		return err
	}
	if err := s.repo.DeleteTeam(ctx, id); err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	return nil
}

func (s *Service) SetMembership(ctx context.Context, in *murphy.TeamMembership) (_ *murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", in.GetTeamId()), telemetry.WithField("user.id", in.GetUserId()))
	defer span.End(&err)

	now := murphy.Now()
	membership := &murphy.TeamMembership{
		Id: murphy.NewID(), TeamId: in.GetTeamId(), UserId: in.GetUserId(), Role: in.GetRole(), CreatedAt: now, UpdatedAt: now,
	}
	if err := membership.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.GetTeam(ctx, membership.GetTeamId()); err != nil {
		return nil, err
	}
	if _, err := s.GetUser(ctx, membership.GetUserId()); err != nil {
		return nil, err
	}
	if err := s.repo.SetMembership(ctx, membership); err != nil {
		return nil, fmt.Errorf("set team membership: %w", err)
	}
	stored, err := s.repo.GetMembership(ctx, membership.GetTeamId(), membership.GetUserId())
	if err != nil {
		return nil, fmt.Errorf("read team membership: %w", err)
	}
	return stored, nil
}

func (s *Service) ListMemberships(ctx context.Context, teamID string) (_ []*murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", teamID))
	defer span.End(&err)

	if _, err := s.GetTeam(ctx, teamID); err != nil {
		return nil, err
	}
	memberships, err := s.repo.ListMemberships(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team memberships: %w", err)
	}
	return memberships, nil
}

func (s *Service) RemoveMembership(ctx context.Context, teamID, userID string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", teamID), telemetry.WithField("user.id", userID))
	defer span.End(&err)

	if err := murphy.LookupID("team", teamID); err != nil {
		return err
	}
	if err := murphy.LookupID("user", userID); err != nil {
		return err
	}
	if err := s.repo.DeleteMembership(ctx, teamID, userID); err != nil {
		return fmt.Errorf("remove team membership: %w", err)
	}
	return nil
}

func (s *Service) ExistingUsers(ctx context.Context, ids []string) (_ map[string]bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return existing(ctx, ids, s.repo.ExistingUsers)
}

func (s *Service) ExistingTeams(ctx context.Context, ids []string) (_ map[string]bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return existing(ctx, ids, s.repo.ExistingTeams)
}

func existing(ctx context.Context, ids []string, find func(ctx context.Context, ids []string) ([]string, error)) (map[string]bool, error) {
	found := map[string]bool{}
	if len(ids) == 0 {
		return found, nil
	}
	rows, err := find(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range rows {
		found[id] = true
	}
	return found, nil
}

func (s *Service) ActiveUsers(ctx context.Context, ids []string) (_ map[string]bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return existing(ctx, ids, s.repo.ActiveUsers)
}

func (s *Service) ContactMethodsOfUsers(ctx context.Context, userIDs []string) (_ map[string][]*murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	byUser := map[string][]*murphy.ContactMethod{}
	if len(userIDs) == 0 {
		return byUser, nil
	}
	methods, err := s.repo.ListContactMethodsOfUsers(ctx, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list contact methods: %w", err)
	}
	for _, method := range methods {
		byUser[method.GetUserId()] = append(byUser[method.GetUserId()], method)
	}
	return byUser, nil
}
