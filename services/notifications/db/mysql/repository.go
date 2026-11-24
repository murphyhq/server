package mysql

import (
	"context"
	"embed"
	"time"

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
		Service:    "notifications",
		Migrations: migrations,
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{tx: tx, queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

func (r *Repository) RecipientsOf(ctx context.Context, incidentID string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	users, err := r.queries.RecipientsOf(ctx, incidentID)
	if err != nil {
		return nil, murphymysql.Error("page recipient", err)
	}
	return users, nil
}

func (r *Repository) DeleteMessages(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return nil
	}
	return murphymysql.Error("scheduled message", r.queries.DeleteMessages(ctx, ids))
}

func (r *Repository) PurgeHandledMessages(ctx context.Context, before time.Time) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	purged, err := r.queries.PurgeHandledMessages(ctx, before.UTC())
	if err != nil {
		return 0, murphymysql.Error("message", err)
	}
	return purged, nil
}
