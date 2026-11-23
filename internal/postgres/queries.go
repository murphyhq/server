package postgres

import (
	"context"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"

	"github.com/murphyhq/server/internal/metrics"
)

var queries = metrics.NewPerScopeCounter("db.queries", "Database queries")

type countingTracer struct {
	*otelpgx.Tracer
}

func (t countingTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	queries.Add(ctx)
	return t.Tracer.TraceQueryStart(ctx, conn, data)
}

func (t countingTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	queries.Add(ctx)
	t.Tracer.TraceBatchQuery(ctx, conn, data)
}

func (t countingTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	queries.Add(ctx)
	return t.Tracer.TraceCopyFromStart(ctx, conn, data)
}
