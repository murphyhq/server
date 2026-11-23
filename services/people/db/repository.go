package db

import (
	"context"

	"github.com/murphyhq/server/services/people"
)

type Engine interface {
	people.Repository
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository struct {
	Engine
}

var _ people.Repository = (*Repository)(nil)
