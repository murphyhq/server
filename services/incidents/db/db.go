package db

import (
	"context"
	"errors"
	"strings"

	"github.com/murphyhq/server/services/incidents"
	"github.com/murphyhq/server/services/incidents/db/postgres"
)

type Storage interface {
	incidents.Repository
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

func Open(ctx context.Context, url string) (Storage, error) {
	scheme, _, _ := strings.Cut(url, "://")
	switch scheme {
	case "postgres", "postgresql":
		return postgres.Open(ctx, url)
	default:
		return nil, errors.New("unsupported database")
	}
}
