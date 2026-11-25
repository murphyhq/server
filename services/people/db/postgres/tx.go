package postgres

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
)

type Tx struct {
	queries *Queries
}

func (t *Tx) CreateUser(ctx context.Context, user *murphy.User) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("user", t.queries.CreateUser(ctx, CreateUserParams{
		ID: user.GetId(), Name: user.GetName(), Email: user.GetEmail(), TimeZone: user.GetTimeZone(),
		Status: user.GetStatus().Text(), Role: user.GetRole().Text(), CreatedAt: user.GetCreatedAt().AsTime(), UpdatedAt: user.GetUpdatedAt().AsTime(),
	}))
}

func (t *Tx) CreateIdentity(ctx context.Context, identity *murphy.Identity) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return insertIdentity(ctx, t.queries, identity)
}

func (t *Tx) AdminExists(ctx context.Context) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return t.queries.AdminExists(ctx)
}

func (t *Tx) SetUserRole(ctx context.Context, id string, role murphy.UserRole, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.SetUserRole(ctx, SetUserRoleParams{ID: id, Role: role.Text(), UpdatedAt: at})
	return murphypostgres.Changed("user", rows, err)
}

func (t *Tx) RevokeSessionsOfUser(ctx context.Context, userID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = t.queries.RevokeSessionsOfUser(ctx, RevokeSessionsOfUserParams{RevokedAt: &at, UpdatedAt: at, UserID: userID})
	return murphypostgres.Error("session", err)
}

func (t *Tx) DiscardOneTimeCodesOfUser(ctx context.Context, userID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = t.queries.DiscardOneTimeCodesOfUser(ctx, DiscardOneTimeCodesOfUserParams{UsedAt: &at, UpdatedAt: at, UserID: userID})
	return murphypostgres.Error("one-time code", err)
}

func (t *Tx) CreateContactMethod(ctx context.Context, method *murphy.ContactMethod) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return insertContactMethod(ctx, t.queries, method)
}
