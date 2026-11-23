package postgres

import (
	"context"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/murphyhq/server/internal/metrics"
)

var (
	queries   = metrics.NewPerScopeCounter("db.queries", "Database queries")
	queryTime = metrics.NewPerScopeDuration("db.time", "Time spent running database queries")
	waitTime  = metrics.NewPerScopeDuration("db.wait", "Time spent waiting for a database connection")
)

type queryStarted struct{}

type acquireStarted struct{}

type countingTracer struct {
	*otelpgx.Tracer
}

func (t countingTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	queries.Add(ctx)
	return t.Tracer.TraceQueryStart(context.WithValue(ctx, queryStarted{}, time.Now()), conn, data)
}

func (t countingTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	addSince(ctx, queryStarted{}, queryTime)
	t.Tracer.TraceQueryEnd(ctx, conn, data)
}

func (t countingTracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	return t.Tracer.TraceBatchStart(context.WithValue(ctx, queryStarted{}, time.Now()), conn, data)
}

func (t countingTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	queries.Add(ctx)
	t.Tracer.TraceBatchQuery(ctx, conn, data)
}

func (t countingTracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	addSince(ctx, queryStarted{}, queryTime)
	t.Tracer.TraceBatchEnd(ctx, conn, data)
}

func (t countingTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	queries.Add(ctx)
	return t.Tracer.TraceCopyFromStart(context.WithValue(ctx, queryStarted{}, time.Now()), conn, data)
}

func (t countingTracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	addSince(ctx, queryStarted{}, queryTime)
	t.Tracer.TraceCopyFromEnd(ctx, conn, data)
}

func (t countingTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	return t.Tracer.TraceAcquireStart(context.WithValue(ctx, acquireStarted{}, time.Now()), pool, data)
}

func (t countingTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	addSince(ctx, acquireStarted{}, waitTime)
	t.Tracer.TraceAcquireEnd(ctx, pool, data)
}

func addSince(ctx context.Context, key any, duration *metrics.PerScopeDuration) {
	if started, ok := ctx.Value(key).(time.Time); ok {
		duration.Add(ctx, time.Since(started))
	}
}
