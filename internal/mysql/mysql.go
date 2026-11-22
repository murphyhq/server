package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/XSAM/otelsql"
	mysqldriver "github.com/go-sql-driver/mysql"
	"go.opentelemetry.io/otel/attribute"
)

type Config struct {
	URL string
}

func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	driverConfig, err := parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return nil, errors.New("not a valid MySQL connection string")
	}

	attributes := otelsql.WithAttributes(attribute.String("db.system.name", "mysql"))
	db := otelsql.OpenDB(connector, attributes,
		otelsql.WithSpanNameFormatter(queryName),
		otelsql.WithSpanOptions(otelsql.SpanOptions{OmitConnResetSession: true, OmitRows: true, OmitConnPrepare: true}),
	)
	db.SetConnMaxLifetime(5 * time.Minute)
	if _, err := otelsql.RegisterDBStatsMetrics(db, attributes); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("record database pool metrics: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	return db, nil
}

func parse(raw string) (*mysqldriver.Config, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return nil, errors.New("not a valid MySQL connection string, expected mysql://user:password@host:3306/database")
	}
	c := mysqldriver.NewConfig()
	c.Net = "tcp"
	c.Addr = u.Host
	c.DBName = strings.Trim(u.Path, "/")
	c.User = u.User.Username()
	c.Passwd, _ = u.User.Password()
	c.ParseTime = true
	c.ClientFoundRows = true
	c.Loc = time.UTC
	return c, nil
}

func queryName(_ context.Context, method otelsql.Method, query string) string {
	if rest, ok := strings.CutPrefix(query, "-- name: "); ok {
		if name, _, found := strings.Cut(rest, " "); found {
			return name
		}
	}
	return string(method)
}
