package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/murphyhq/server/internal/tracer"
)

type DatabaseConfig[T any] struct {
	URL        string
	Service    string
	Migrations fs.FS
	Tx         func(tx DBTX) T
}

type Database[T any] struct {
	db         *sql.DB
	service    string
	migrations fs.FS
	tx         func(tx DBTX) T
}

func OpenDatabase[T any](ctx context.Context, cfg DatabaseConfig[T]) (*Database[T], error) {
	db, err := Open(ctx, Config{URL: cfg.URL})
	if err != nil {
		return nil, err
	}
	return &Database[T]{db: db, service: cfg.Service, migrations: cfg.Migrations, tx: cfg.Tx}, nil
}

func (d *Database[T]) DB() DBTX {
	return counting{db: d.db}
}

func (d *Database[T]) Migrate(ctx context.Context) error {
	files, err := fs.Sub(d.migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load %s migrations: %w", d.service, err)
	}
	return Migrate(ctx, d.db, Migrations{Service: d.service, Files: files})
}

func (d *Database[T]) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *Database[T]) Close() {
	_ = d.db.Close()
}

func (d *Database[T]) Transaction(ctx context.Context, work func(tx T) error) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := work(d.tx(counting{db: tx})); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
