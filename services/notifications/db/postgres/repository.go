package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5"

	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	*murphypostgres.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphypostgres.OpenDatabase(ctx, murphypostgres.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "notifications",
		Migrations: migrations,
		Tx:         func(tx pgx.Tx) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.Pool())}, nil
}

func (r *Repository) RecipientsOf(ctx context.Context, incidentID string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	users, err := r.queries.RecipientsOf(ctx, incidentID)
	if err != nil {
		return nil, murphypostgres.Error("page recipient", err)
	}
	return users, nil
}

func (r *Repository) DeleteMessages(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("scheduled message", r.queries.DeleteMessages(ctx, ids))
}

func (r *Repository) PurgeHandledMessages(ctx context.Context, before time.Time) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	purged, err := r.queries.PurgeHandledMessages(ctx, before.UTC())
	if err != nil {
		return 0, murphypostgres.Error("message", err)
	}
	return purged, nil
}
