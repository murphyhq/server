package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/XSAM/otelsql"
	mysqldriver "github.com/go-sql-driver/mysql"
	"go.opentelemetry.io/otel/attribute"

	"github.com/murphyhq/server/internal/telemetry"
)

type Config struct {
	URL string
}

func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	driverConfig, pool, err := parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return nil, errors.New("not a valid MySQL connection string")
	}

	attributes := otelsql.WithAttributes(append([]attribute.KeyValue{attribute.String("db.system.name", "mysql")}, telemetry.ComponentAttributes(ctx)...)...)
	db := otelsql.OpenDB(connector, attributes,
		otelsql.WithSpanNameFormatter(queryName),
		otelsql.WithSpanOptions(otelsql.SpanOptions{OmitConnResetSession: true, OmitRows: true, OmitConnPrepare: true}),
	)
	db.SetMaxOpenConns(pool.maxConns)
	db.SetMaxIdleConns(pool.maxConns)
	db.SetConnMaxLifetime(pool.maxConnLifetime)
	db.SetConnMaxIdleTime(pool.maxConnIdleTime)
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

const DefaultMaxConns = 4

type pool struct {
	maxConns        int
	maxConnLifetime time.Duration
	maxConnIdleTime time.Duration
}

func parse(raw string) (*mysqldriver.Config, pool, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return nil, pool{}, errors.New("not a valid MySQL connection string, expected mysql://user:password@host:3306/database")
	}
	p, err := parsePool(u.Query())
	if err != nil {
		return nil, pool{}, err
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
	return c, p, nil
}

func parsePool(query url.Values) (pool, error) {
	p := pool{maxConns: DefaultMaxConns, maxConnLifetime: time.Hour, maxConnIdleTime: 30 * time.Minute}
	if raw := query.Get("pool_max_conns"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return pool{}, errors.New("pool_max_conns must be a whole number of at least 1")
		}
		p.maxConns = n
	}
	for name, target := range map[string]*time.Duration{"pool_max_conn_lifetime": &p.maxConnLifetime, "pool_max_conn_idle_time": &p.maxConnIdleTime} {
		if raw := query.Get(name); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 {
				return pool{}, errors.New(name + " must be a positive duration, such as 30m")
			}
			*target = d
		}
	}
	return p, nil
}

func queryName(_ context.Context, method otelsql.Method, query string) string {
	if rest, ok := strings.CutPrefix(query, "-- name: "); ok {
		if name, _, found := strings.Cut(rest, " "); found {
			return name
		}
	}
	return string(method)
}
