package db

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/people"
)

type Tx interface {
	CreateUser(ctx context.Context, user *murphy.User) error
	CreateIdentity(ctx context.Context, identity *murphy.Identity) error
	AdminExists(ctx context.Context) (bool, error)
	SetUserRole(ctx context.Context, id string, role murphy.UserRole, at time.Time) error
}

type Engine[T Tx] interface {
	people.Repository
	GetUserByEmail(ctx context.Context, email string) (*murphy.User, error)
	AdminExists(ctx context.Context) (bool, error)
	GetIdentity(ctx context.Context, issuer, subject string) (*murphy.Identity, error)
	CreateIdentity(ctx context.Context, identity *murphy.Identity) error
	CreateSession(ctx context.Context, session *murphy.Session) error
	GetActiveSession(ctx context.Context, tokenHash []byte, now time.Time) (*murphy.Session, error)
	RevokeSession(ctx context.Context, tokenHash []byte, at time.Time) error
	CreateSignIn(ctx context.Context, signIn *murphy.SignIn) error
	UseSignIn(ctx context.Context, stateHash []byte, now time.Time) (bool, error)
	GetSignIn(ctx context.Context, stateHash []byte) (*murphy.SignIn, error)
	DeleteExpiredSignIns(ctx context.Context, before time.Time) error
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var (
	_ people.Repository     = (*Repository[Tx])(nil)
	_ people.AuthRepository = (*Repository[Tx])(nil)
)

func (r *Repository[T]) StartSignIn(ctx context.Context, signIn *murphy.SignIn, now time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := r.DeleteExpiredSignIns(ctx, now); err != nil {
		return err
	}
	return r.CreateSignIn(ctx, signIn)
}

func (r *Repository[T]) TakeSignIn(ctx context.Context, stateHash []byte, now time.Time) (_ *murphy.SignIn, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	used, err := r.UseSignIn(ctx, stateHash, now)
	if err != nil {
		return nil, err
	}
	if !used {
		return nil, &murphy.NotFoundError{Entity: "sign-in"}
	}
	return r.GetSignIn(ctx, stateHash)
}

func (r *Repository[T]) CreateUserWithIdentity(ctx context.Context, user *murphy.User, identity *murphy.Identity) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Transaction(ctx, func(tx T) error {
		if err := tx.CreateUser(ctx, user); err != nil {
			return err
		}
		return tx.CreateIdentity(ctx, identity)
	})
}

func (r *Repository[T]) ClaimFirstAdmin(ctx context.Context, userID string, now time.Time) (claimed bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	err = r.Transaction(ctx, func(tx T) error {
		exists, err := tx.AdminExists(ctx)
		if err != nil || exists {
			return err
		}
		claimed = true
		return tx.SetUserRole(ctx, userID, murphy.UserRole_USER_ROLE_ADMIN, now)
	})
	return claimed && err == nil, err
}
