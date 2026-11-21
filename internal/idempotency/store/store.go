package store

import (
	"context"
	"errors"
	"strings"

	"github.com/murphyhq/server/internal/idempotency"
	"github.com/murphyhq/server/internal/idempotency/store/redis"
)

type Store interface {
	idempotency.Store
	Ping(ctx context.Context) error
	Close() error
}

func Open(url string) (Store, error) {
	scheme, _, _ := strings.Cut(url, "://")
	switch scheme {
	case "redis", "rediss":
		return redis.Open(url)
	default:
		return nil, errors.New("unsupported store")
	}
}
