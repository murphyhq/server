package mysql

import (
	"database/sql"
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/murphyhq/server"
)

const duplicateEntry = 1062

func Error(entity string, err error) error {
	var mysqlErr *mysqldriver.MySQLError
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return &murphy.NotFoundError{Entity: entity}
	case errors.As(err, &mysqlErr) && mysqlErr.Number == duplicateEntry:
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
