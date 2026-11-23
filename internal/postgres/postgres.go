package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultMaxConns = 4

type Config struct {
	URL string
}

func Open(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, errors.New("not a valid PostgreSQL connection string")
	}

	if u, err := url.Parse(cfg.URL); err == nil && !u.Query().Has("pool_max_conns") {
		poolConfig.MaxConns = DefaultMaxConns
	}
	poolConfig.ConnConfig.Tracer = countingTracer{otelpgx.NewTracer(otelpgx.WithSpanNameFunc(queryName))}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("record database pool metrics: %w", err)
	}
	return pool, nil
}

func queryName(statement string) string {
	if rest, ok := strings.CutPrefix(statement, "-- name: "); ok {
		if name, _, found := strings.Cut(rest, " "); found {
			return name
		}
	}
	return otelpgx.SQLOperationName(context.Background(), statement)
}
