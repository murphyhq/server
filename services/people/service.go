package people

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

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
	DisableContactMethod(ctx context.Context, method *murphy.ContactMethod) error
	CreateTeam(ctx context.Context, team *murphy.Team) error
	GetTeam(ctx context.Context, id string) (*murphy.Team, error)
	ListTeams(ctx context.Context, before string, limit int) ([]*murphy.Team, error)
	UpdateTeam(ctx context.Context, team *murphy.Team) error
	DeleteTeam(ctx context.Context, id string) error
	SetMembership(ctx context.Context, membership *murphy.TeamMembership) error
	GetMembership(ctx context.Context, teamID, userID string) (*murphy.TeamMembership, error)
	ListMemberships(ctx context.Context, teamID string) ([]*murphy.TeamMembership, error)
	DeleteMembership(ctx context.Context, teamID, userID string) error
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	TeamNames(ctx context.Context, ids []string) (map[string]string, error)
	ActiveUsers(ctx context.Context, ids []string) ([]string, error)
	ListMembershipsOfUser(ctx context.Context, userID string) ([]*murphy.TeamMembership, error)
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
		Role:      murphy.Or(in.GetRole(), murphy.UserRole_USER_ROLE_MEMBER),
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
		Role:      current.GetRole(),
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
		Id:           murphy.NewID(),
		UserId:       in.GetUserId(),
		Type:         in.GetType(),
		Address:      in.GetAddress(),
		PushPlatform: in.GetPushPlatform(),
		CreatedAt:    now,
		UpdatedAt:    now,
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

func (s *Service) ListContactMethods(ctx context.Context, userID string) (_ *murphy.User, _ []*murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	methods, err := s.repo.ListContactMethods(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list contact methods: %w", err)
	}
	return user, methods, nil
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
		Id:           current.GetId(),
		UserId:       current.GetUserId(),
		Type:         murphy.Or(in.GetType(), current.GetType()),
		Address:      in.GetAddress(),
		PushPlatform: murphy.Or(in.GetPushPlatform(), current.GetPushPlatform()),
		CreatedAt:    current.GetCreatedAt(),
		UpdatedAt:    murphy.Now(),
	}
	if method.GetAddress() == current.GetAddress() {
		method.DisabledAt, method.DisabledReason = current.GetDisabledAt(), current.GetDisabledReason()
	}
	if err := method.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateContactMethod(ctx, method); err != nil {
		return nil, fmt.Errorf("update contact method: %w", err)
	}
	return method, nil
}

func (s *Service) DisableContactMethod(ctx context.Context, method *murphy.ContactMethod, reason string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("contact_method.id", method.GetId()))
	defer span.End(&err)

	disabled := &murphy.ContactMethod{Id: method.GetId(), Address: method.GetAddress(), DisabledAt: timestamppb.New(at), DisabledReason: reason}
	if err := s.repo.DisableContactMethod(ctx, disabled); err != nil {
		return fmt.Errorf("disable contact method: %w", err)
	}
	return nil
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

func (s *Service) ListMemberships(ctx context.Context, teamID string) (_ *murphy.Team, _ []*murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("team.id", teamID))
	defer span.End(&err)

	team, err := s.GetTeam(ctx, teamID)
	if err != nil {
		return nil, nil, err
	}
	memberships, err := s.repo.ListMemberships(ctx, teamID)
	if err != nil {
		return nil, nil, fmt.Errorf("list team memberships: %w", err)
	}
	return team, memberships, nil
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

func (s *Service) UserNames(ctx context.Context, ids []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return s.repo.UserNames(ctx, ids)
}

func (s *Service) TeamNames(ctx context.Context, ids []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return s.repo.TeamNames(ctx, ids)
}

func (s *Service) ActiveUsers(ctx context.Context, ids []string) (_ map[string]bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	found := map[string]bool{}
	if len(ids) == 0 {
		return found, nil
	}
	active, err := s.repo.ActiveUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range active {
		found[id] = true
	}
	return found, nil
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

func (s *Service) ListMembershipsOfUser(ctx context.Context, userID string) (_ *murphy.User, _ []*murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	memberships, err := s.repo.ListMembershipsOfUser(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("list the user's memberships: %w", err)
	}
	return user, memberships, nil
}

func (s *Service) TeamRolesOf(ctx context.Context, userID string) (_ map[string]murphy.TeamRole, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("user.id", userID))
	defer span.End(&err)

	memberships, err := s.repo.ListMembershipsOfUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list the user's team roles: %w", err)
	}
	roles := make(map[string]murphy.TeamRole, len(memberships))
	for _, membership := range memberships {
		roles[membership.GetTeamId()] = membership.GetRole()
	}
	return roles, nil
}
