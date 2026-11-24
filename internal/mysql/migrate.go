package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Migrations struct {
	Service string
	Files   fs.FS
}

func Migrate(ctx context.Context, db *sql.DB, sets ...Migrations) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	for _, set := range sets {
		if err := migrate(ctx, db, set); err != nil {
			return err
		}
	}
	return nil
}

func migrate(ctx context.Context, db *sql.DB, set Migrations) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("service", set.Service))
	defer span.End(&err)

	store, err := database.NewStore(database.DialectMySQL, set.Service+"_migrations")
	if err != nil {
		return fmt.Errorf("create migration store for %s: %w", set.Service, err)
	}
	locker, err := lock.NewMySQLTableLocker(lock.WithTableName(set.Service + "_migration_locks"))
	if err != nil {
		return fmt.Errorf("create migration lock for %s: %w", set.Service, err)
	}
	provider, err := goose.NewProvider(database.DialectCustom, db, set.Files, goose.WithStore(store), goose.WithLocker(locker))
	if err != nil {
		return fmt.Errorf("load migrations for %s: %w", set.Service, err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations for %s: %w", set.Service, err)
	}
	logger.Info(ctx, "migrations applied", telemetry.WithField("count", len(results)))
	return nil
}
