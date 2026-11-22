package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/murphyhq/server"
)

const uniqueViolation = "23505"

func Error(entity string, err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return &murphy.NotFoundError{Entity: entity}
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation:
		return murphy.ErrConflict
	}
	return err
}

func Changed(entity string, rows int64, err error) error {
	if err != nil {
		return Error(entity, err)
	}
	if rows == 0 {
		return &murphy.NotFoundError{Entity: entity}
	}
	return nil
}
