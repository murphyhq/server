package mysql

import (
	"context"
	"database/sql"

	"github.com/murphyhq/server/internal/metrics"
)

var queries = metrics.NewPerScopeCounter("db.queries", "Database queries")

type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type counting struct {
	db DBTX
}

func (c counting) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	queries.Add(ctx)
	return c.db.ExecContext(ctx, query, args...)
}

func (c counting) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return c.db.PrepareContext(ctx, query)
}

func (c counting) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	queries.Add(ctx)
	return c.db.QueryContext(ctx, query, args...)
}

func (c counting) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	queries.Add(ctx)
	return c.db.QueryRowContext(ctx, query, args...)
}
