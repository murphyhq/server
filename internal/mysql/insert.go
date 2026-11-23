package mysql

import (
	"context"
	"database/sql"
	"strings"
)

type Rows struct {
	Name    string
	Table   string
	Columns []string
	Values  [][]any
}

func Insert(ctx context.Context, tx *sql.Tx, rows Rows) error {
	if len(rows.Values) == 0 {
		return nil
	}
	var query strings.Builder
	query.WriteString("-- name: ")
	query.WriteString(rows.Name)
	query.WriteString(" :exec\nINSERT INTO ")
	query.WriteString(rows.Table)
	query.WriteString(" (")
	query.WriteString(strings.Join(rows.Columns, ", "))
	query.WriteString(") VALUES ")
	args := make([]any, 0, len(rows.Values)*len(rows.Columns))
	for i, row := range rows.Values {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteByte('(')
		for j := range rows.Columns {
			if j > 0 {
				query.WriteString(", ")
			}
			query.WriteByte('?')
		}
		query.WriteByte(')')
		args = append(args, row...)
	}
	_, err := tx.ExecContext(ctx, query.String(), args...)
	return err
}
