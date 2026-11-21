package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/tracer"
)

type Migrations struct {
	Service string
	Files   fs.FS
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, sets ...Migrations) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()

	for _, set := range sets {
		if err := migrate(ctx, db, set); err != nil {
			return err
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB, set Migrations) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, logger.WithField("service", set.Service))
	defer span.End(&err)

	store, err := database.NewStore(database.DialectPostgres, set.Service+"_migrations")
	if err != nil {
		return fmt.Errorf("create migration store for %s: %w", set.Service, err)
	}
	provider, err := goose.NewProvider(database.DialectCustom, db, set.Files, goose.WithStore(store))
	if err != nil {
		return fmt.Errorf("load migrations for %s: %w", set.Service, err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations for %s: %w", set.Service, err)
	}
	logger.Info(ctx, "migrations applied", logger.WithField("count", len(results)))
	return nil
}
