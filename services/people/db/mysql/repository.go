package mysql

import (
	"context"
	"embed"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	*murphymysql.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphymysql.OpenDatabase(ctx, murphymysql.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "people",
		Migrations: migrations,
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

func (r *Repository) CreateUser(ctx context.Context, user *murphy.User) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("user", r.queries.CreateUser(ctx, CreateUserParams{
		ID: user.GetId(), Name: user.GetName(), Email: user.GetEmail(), TimeZone: user.GetTimeZone(),
		Status: user.GetStatus().Text(), Role: user.GetRole().Text(), CreatedAt: user.GetCreatedAt().AsTime(), UpdatedAt: user.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetUser(ctx context.Context, id string) (_ *murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetUser(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("user", err)
	}
	return toUser(row), nil
}

func (r *Repository) ListUsers(ctx context.Context, before string, limit int) (_ []*murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListUsers(ctx, ListUsersParams{Before: before, Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("user", err)
	}
	users := make([]*murphy.User, len(rows))
	for i, row := range rows {
		users[i] = toUser(row)
	}
	return users, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (_ *murphy.User, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, murphymysql.Error("user", err)
	}
	return toUser(row), nil
}

func (r *Repository) AdminExists(ctx context.Context) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.queries.AdminExists(ctx)
}

func (r *Repository) UpdateUser(ctx context.Context, user *murphy.User) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UpdateUser(ctx, UpdateUserParams{
		ID: user.GetId(), Name: user.GetName(), Email: user.GetEmail(), TimeZone: user.GetTimeZone(),
		Status: user.GetStatus().Text(), UpdatedAt: user.GetUpdatedAt().AsTime(),
	})
	return murphymysql.Changed("user", rows, err)
}

func (r *Repository) DeleteUser(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteUser(ctx, id)
	return murphymysql.Changed("user", rows, err)
}

func (r *Repository) CreateContactMethod(ctx context.Context, method *murphy.ContactMethod) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("contact method", r.queries.CreateContactMethod(ctx, CreateContactMethodParams{
		ID: method.GetId(), UserID: method.GetUserId(), Type: method.GetType().Text(), Address: method.GetAddress(),
		CreatedAt: method.GetCreatedAt().AsTime(), UpdatedAt: method.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetContactMethod(ctx context.Context, id string) (_ *murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetContactMethod(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("contact method", err)
	}
	return toContactMethod(row), nil
}

func (r *Repository) ListContactMethods(ctx context.Context, userID string) (_ []*murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListContactMethods(ctx, userID)
	if err != nil {
		return nil, murphymysql.Error("contact method", err)
	}
	methods := make([]*murphy.ContactMethod, len(rows))
	for i, row := range rows {
		methods[i] = toContactMethod(row)
	}
	return methods, nil
}

func (r *Repository) ListContactMethodsOfUsers(ctx context.Context, userIDs []string) (_ []*murphy.ContactMethod, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListContactMethodsOfUsers(ctx, userIDs)
	if err != nil {
		return nil, murphymysql.Error("contact method", err)
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
	return murphymysql.Changed("contact method", rows, err)
}

func (r *Repository) DeleteContactMethod(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteContactMethod(ctx, id)
	return murphymysql.Changed("contact method", rows, err)
}

func toUser(row User) *murphy.User {
	return &murphy.User{
		Id: row.ID, Name: row.Name, Email: row.Email, TimeZone: row.TimeZone, Status: murphy.UserStatusFromText(row.Status),
		Role: murphy.UserRoleFromText(row.Role), CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
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

	return murphymysql.Error("team", r.queries.CreateTeam(ctx, CreateTeamParams{
		ID: team.GetId(), Name: team.GetName(), CreatedAt: team.GetCreatedAt().AsTime(), UpdatedAt: team.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetTeam(ctx context.Context, id string) (_ *murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetTeam(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("team", err)
	}
	return toTeam(row), nil
}

func (r *Repository) ListTeams(ctx context.Context, before string, limit int) (_ []*murphy.Team, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListTeams(ctx, ListTeamsParams{Before: before, Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("team", err)
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
	return murphymysql.Changed("team", rows, err)
}

func (r *Repository) DeleteTeam(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteTeam(ctx, id)
	return murphymysql.Changed("team", rows, err)
}

func (r *Repository) SetMembership(ctx context.Context, membership *murphy.TeamMembership) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("team membership", r.queries.SetMembership(ctx, SetMembershipParams{
		ID: membership.GetId(), TeamID: membership.GetTeamId(), UserID: membership.GetUserId(), Role: membership.GetRole().Text(),
		CreatedAt: membership.GetCreatedAt().AsTime(), UpdatedAt: membership.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetMembership(ctx context.Context, teamID, userID string) (_ *murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetMembership(ctx, GetMembershipParams{TeamID: teamID, UserID: userID})
	if err != nil {
		return nil, murphymysql.Error("team membership", err)
	}
	return toMembership(row), nil
}

func (r *Repository) ListMemberships(ctx context.Context, teamID string) (_ []*murphy.TeamMembership, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListMemberships(ctx, teamID)
	if err != nil {
		return nil, murphymysql.Error("team membership", err)
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
	return murphymysql.Changed("team membership", rows, err)
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
		return nil, murphymysql.Error("user", err)
	}
	return existing, nil
}

func (r *Repository) ExistingTeams(ctx context.Context, ids []string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	existing, err := r.queries.ExistingTeams(ctx, ids)
	if err != nil {
		return nil, murphymysql.Error("team", err)
	}
	return existing, nil
}

func (r *Repository) ActiveUsers(ctx context.Context, ids []string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	active, err := r.queries.ActiveUsers(ctx, ids)
	if err != nil {
		return nil, murphymysql.Error("user", err)
	}
	return active, nil
}

func (r *Repository) GetIdentity(ctx context.Context, issuer, subject string) (_ *murphy.Identity, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIdentity(ctx, GetIdentityParams{Issuer: issuer, Subject: subject})
	if err != nil {
		return nil, murphymysql.Error("identity", err)
	}
	return &murphy.Identity{
		Id: row.ID, UserId: row.UserID, Issuer: row.Issuer, Subject: row.Subject,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}, nil
}

func (r *Repository) CreateIdentity(ctx context.Context, identity *murphy.Identity) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return insertIdentity(ctx, r.queries, identity)
}

func (r *Repository) CreateSession(ctx context.Context, session *murphy.Session) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("session", r.queries.CreateSession(ctx, CreateSessionParams{
		ID: session.GetId(), UserID: session.GetUserId(), TokenHash: session.GetTokenHash(), ExpiresAt: session.GetExpiresAt().AsTime(),
		CreatedAt: session.GetCreatedAt().AsTime(), UpdatedAt: session.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetActiveSession(ctx context.Context, tokenHash []byte, now time.Time) (_ *murphy.Session, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetActiveSession(ctx, GetActiveSessionParams{TokenHash: tokenHash, Now: now})
	if err != nil {
		return nil, murphymysql.Error("session", err)
	}
	return &murphy.Session{
		Id: row.ID, UserId: row.UserID, TokenHash: row.TokenHash, ExpiresAt: timestamppb.New(row.ExpiresAt),
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}, nil
}

func (r *Repository) RevokeSession(ctx context.Context, tokenHash []byte, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.RevokeSession(ctx, RevokeSessionParams{RevokedAt: &at, UpdatedAt: at, TokenHash: tokenHash})
	return murphymysql.Changed("session", rows, err)
}

func (r *Repository) CreateSignIn(ctx context.Context, signIn *murphy.SignIn) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("sign-in", r.queries.CreateSignIn(ctx, CreateSignInParams{
		ID: signIn.GetId(), StateHash: signIn.GetStateHash(), Nonce: signIn.GetNonce(), Verifier: signIn.GetVerifier(),
		ReturnTo: database.Text(signIn.GetReturnTo()), CodeChallenge: database.Text(signIn.GetCodeChallenge()),
		ExpiresAt: signIn.GetExpiresAt().AsTime(),
		CreatedAt: signIn.GetCreatedAt().AsTime(), UpdatedAt: signIn.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) UseSignIn(ctx context.Context, stateHash []byte, now time.Time) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UseSignIn(ctx, UseSignInParams{UsedAt: &now, UpdatedAt: now, StateHash: stateHash, Now: now})
	if err != nil {
		return false, murphymysql.Error("sign-in", err)
	}
	return rows == 1, nil
}

func (r *Repository) GetSignIn(ctx context.Context, stateHash []byte) (_ *murphy.SignIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetSignIn(ctx, stateHash)
	if err != nil {
		return nil, murphymysql.Error("sign-in", err)
	}
	return &murphy.SignIn{
		Id: row.ID, StateHash: row.StateHash, Nonce: row.Nonce, Verifier: row.Verifier, ReturnTo: database.TextValue(row.ReturnTo), CodeChallenge: database.TextValue(row.CodeChallenge),
		ExpiresAt: timestamppb.New(row.ExpiresAt), CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}, nil
}

func (r *Repository) DeleteExpiredSignIns(ctx context.Context, before time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = r.queries.DeleteExpiredSignIns(ctx, before)
	return err
}

func insertIdentity(ctx context.Context, queries *Queries, identity *murphy.Identity) error {
	return murphymysql.Error("identity", queries.CreateIdentity(ctx, CreateIdentityParams{
		ID: identity.GetId(), UserID: identity.GetUserId(), Issuer: identity.GetIssuer(), Subject: identity.GetSubject(),
		CreatedAt: identity.GetCreatedAt().AsTime(), UpdatedAt: identity.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) CreateOneTimeCode(ctx context.Context, code *murphy.OneTimeCode) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("one-time code", r.queries.CreateOneTimeCode(ctx, CreateOneTimeCodeParams{
		ID: code.GetId(), CodeHash: code.GetCodeHash(), UserID: code.GetUserId(), Purpose: code.GetPurpose().Text(),
		CodeChallenge: database.Text(code.GetCodeChallenge()), ExpiresAt: code.GetExpiresAt().AsTime(),
		CreatedAt: code.GetCreatedAt().AsTime(), UpdatedAt: code.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) UseOneTimeCode(ctx context.Context, codeHash []byte, now time.Time) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UseOneTimeCode(ctx, UseOneTimeCodeParams{UsedAt: &now, UpdatedAt: now, CodeHash: codeHash, Now: now})
	if err != nil {
		return false, murphymysql.Error("one-time code", err)
	}
	return rows == 1, nil
}

func (r *Repository) GetOneTimeCode(ctx context.Context, codeHash []byte) (_ *murphy.OneTimeCode, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetOneTimeCode(ctx, codeHash)
	if err != nil {
		return nil, murphymysql.Error("one-time code", err)
	}
	return &murphy.OneTimeCode{
		Id: row.ID, CodeHash: row.CodeHash, UserId: row.UserID, Purpose: murphy.OneTimeCodePurposeFromText(row.Purpose),
		CodeChallenge: database.TextValue(row.CodeChallenge), ExpiresAt: timestamppb.New(row.ExpiresAt),
		UsedAt: database.Timestamp(row.UsedAt), CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}, nil
}
