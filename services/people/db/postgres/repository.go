package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	pool    *pgxpool.Pool
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	pool, err := murphypostgres.Open(ctx, murphypostgres.Config{URL: url})
	if err != nil {
		return nil, err
	}
	return &Repository{pool: pool, queries: New(pool)}, nil
}

func (r *Repository) Migrate(ctx context.Context) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load people migrations: %w", err)
	}
	return murphypostgres.Migrate(ctx, r.pool, murphypostgres.Migrations{Service: "people", Files: files})
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) CreateUser(ctx context.Context, user *murphy.User) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("user", r.queries.CreateUser(ctx, CreateUserParams{
		ID: user.GetId(), Name: user.GetName(), Email: user.GetEmail(), TimeZone: user.GetTimeZone(),
		Status: user.GetStatus().Text(), CreatedAt: user.GetCreatedAt().AsTime(), UpdatedAt: user.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetUser(ctx context.Context, id string) (_ *murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetUser(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("user", err)
	}
	return toUser(row), nil
}

func (r *Repository) ListUsers(ctx context.Context, before string, limit int) (_ []*murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListUsers(ctx, ListUsersParams{Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("user", err)
	}
	users := make([]*murphy.User, len(rows))
	for i, row := range rows {
		users[i] = toUser(row)
	}
	return users, nil
}

func (r *Repository) UpdateUser(ctx context.Context, user *murphy.User) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UpdateUser(ctx, UpdateUserParams{
		ID: user.GetId(), Name: user.GetName(), Email: user.GetEmail(), TimeZone: user.GetTimeZone(),
		Status: user.GetStatus().Text(), UpdatedAt: user.GetUpdatedAt().AsTime(),
	})
	return murphypostgres.Changed("user", rows, err)
}

func (r *Repository) DeleteUser(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteUser(ctx, id)
	return murphypostgres.Changed("user", rows, err)
}

func (r *Repository) CreateContactMethod(ctx context.Context, method *murphy.ContactMethod) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("contact method", r.queries.CreateContactMethod(ctx, CreateContactMethodParams{
		ID: method.GetId(), UserID: method.GetUserId(), Type: method.GetType().Text(), Address: method.GetAddress(),
		CreatedAt: method.GetCreatedAt().AsTime(), UpdatedAt: method.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetContactMethod(ctx context.Context, id string) (_ *murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetContactMethod(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("contact method", err)
	}
	return toContactMethod(row), nil
}

func (r *Repository) ListContactMethods(ctx context.Context, userID string) (_ []*murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListContactMethods(ctx, userID)
	if err != nil {
		return nil, murphypostgres.Error("contact method", err)
	}
	methods := make([]*murphy.ContactMethod, len(rows))
	for i, row := range rows {
		methods[i] = toContactMethod(row)
	}
	return methods, nil
}

func (r *Repository) UpdateContactMethod(ctx context.Context, method *murphy.ContactMethod) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UpdateContactMethod(ctx, UpdateContactMethodParams{
		ID: method.GetId(), Type: method.GetType().Text(), Address: method.GetAddress(), UpdatedAt: method.GetUpdatedAt().AsTime(),
	})
	return murphypostgres.Changed("contact method", rows, err)
}

func (r *Repository) DeleteContactMethod(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteContactMethod(ctx, id)
	return murphypostgres.Changed("contact method", rows, err)
}

func toUser(row User) *murphy.User {
	return &murphy.User{
		Id: row.ID, Name: row.Name, Email: row.Email, TimeZone: row.TimeZone, Status: murphy.UserStatusFromText(row.Status),
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}

func toContactMethod(row ContactMethod) *murphy.ContactMethod {
	return &murphy.ContactMethod{
		Id: row.ID, UserId: row.UserID, Type: murphy.ContactMethodTypeFromText(row.Type), Address: row.Address,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}

func (r *Repository) CreateTeam(ctx context.Context, team *murphy.Team) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("team", r.queries.CreateTeam(ctx, CreateTeamParams{
		ID: team.GetId(), Name: team.GetName(), CreatedAt: team.GetCreatedAt().AsTime(), UpdatedAt: team.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetTeam(ctx context.Context, id string) (_ *murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetTeam(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("team", err)
	}
	return toTeam(row), nil
}

func (r *Repository) ListTeams(ctx context.Context, before string, limit int) (_ []*murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListTeams(ctx, ListTeamsParams{Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("team", err)
	}
	teams := make([]*murphy.Team, len(rows))
	for i, row := range rows {
		teams[i] = toTeam(row)
	}
	return teams, nil
}

func (r *Repository) UpdateTeam(ctx context.Context, team *murphy.Team) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UpdateTeam(ctx, UpdateTeamParams{ID: team.GetId(), Name: team.GetName(), UpdatedAt: team.GetUpdatedAt().AsTime()})
	return murphypostgres.Changed("team", rows, err)
}

func (r *Repository) DeleteTeam(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteTeam(ctx, id)
	return murphypostgres.Changed("team", rows, err)
}

func (r *Repository) SetMembership(ctx context.Context, membership *murphy.TeamMembership) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("team membership", r.queries.SetMembership(ctx, SetMembershipParams{
		ID: membership.GetId(), TeamID: membership.GetTeamId(), UserID: membership.GetUserId(), Role: membership.GetRole().Text(),
		CreatedAt: membership.GetCreatedAt().AsTime(), UpdatedAt: membership.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetMembership(ctx context.Context, teamID, userID string) (_ *murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetMembership(ctx, GetMembershipParams{TeamID: teamID, UserID: userID})
	if err != nil {
		return nil, murphypostgres.Error("team membership", err)
	}
	return toMembership(row), nil
}

func (r *Repository) ListMemberships(ctx context.Context, teamID string) (_ []*murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListMemberships(ctx, teamID)
	if err != nil {
		return nil, murphypostgres.Error("team membership", err)
	}
	memberships := make([]*murphy.TeamMembership, len(rows))
	for i, row := range rows {
		memberships[i] = toMembership(row)
	}
	return memberships, nil
}

func (r *Repository) DeleteMembership(ctx context.Context, teamID, userID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteMembership(ctx, DeleteMembershipParams{TeamID: teamID, UserID: userID})
	return murphypostgres.Changed("team membership", rows, err)
}

func toTeam(row Team) *murphy.Team {
	return &murphy.Team{Id: row.ID, Name: row.Name, CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt)}
}

func toMembership(row TeamMembership) *murphy.TeamMembership {
	return &murphy.TeamMembership{
		Id: row.ID, TeamId: row.TeamID, UserId: row.UserID, Role: murphy.TeamRoleFromText(row.Role),
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}

func (r *Repository) ExistingUsers(ctx context.Context, ids []string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	existing, err := r.queries.ExistingUsers(ctx, ids)
	if err != nil {
		return nil, murphypostgres.Error("user", err)
	}
	return existing, nil
}

func (r *Repository) ExistingTeams(ctx context.Context, ids []string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	existing, err := r.queries.ExistingTeams(ctx, ids)
	if err != nil {
		return nil, murphypostgres.Error("team", err)
	}
	return existing, nil
}
