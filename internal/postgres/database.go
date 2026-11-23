package postgres

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/murphyhq/server/internal/tracer"
)

type DatabaseConfig[T any] struct {
	URL        string
	Service    string
	Migrations fs.FS
	Tx         func(tx pgx.Tx) T
}

type Database[T any] struct {
	pool       *pgxpool.Pool
	service    string
	migrations fs.FS
	tx         func(tx pgx.Tx) T
}

func OpenDatabase[T any](ctx context.Context, cfg DatabaseConfig[T]) (*Database[T], error) {
	pool, err := Open(ctx, Config{URL: cfg.URL})
	if err != nil {
		return nil, err
	}
	return &Database[T]{pool: pool, service: cfg.Service, migrations: cfg.Migrations, tx: cfg.Tx}, nil
}

func (d *Database[T]) Pool() *pgxpool.Pool {
	return d.pool
}

func (d *Database[T]) Migrate(ctx context.Context) error {
	files, err := fs.Sub(d.migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load %s migrations: %w", d.service, err)
	}
	return Migrate(ctx, d.pool, Migrations{Service: d.service, Files: files})
}

func (d *Database[T]) Ping(ctx context.Context) error {
	return d.pool.Ping(ctx)
}

func (d *Database[T]) Close() {
	d.pool.Close()
}

func (d *Database[T]) Transaction(ctx context.Context, work func(tx T) error) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := work(d.tx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
