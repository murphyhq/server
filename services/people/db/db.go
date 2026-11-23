package db

import (
	"context"
	"errors"
	"strings"

	"github.com/murphyhq/server/services/people"
	"github.com/murphyhq/server/services/people/db/mysql"
	"github.com/murphyhq/server/services/people/db/postgres"
)

type Storage interface {
	people.Repository
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

func Open(ctx context.Context, url string) (Storage, error) {
	scheme, _, _ := strings.Cut(url, "://")
	switch scheme {
	case "postgres", "postgresql":
		engine, err := postgres.Open(ctx, url)
		if err != nil {
			return nil, err
		}
		return &Repository{Engine: engine}, nil
	case "mysql":
		engine, err := mysql.Open(ctx, url)
		if err != nil {
			return nil, err
		}
		return &Repository{Engine: engine}, nil
	default:
		return nil, errors.New("unsupported database")
	}
}
