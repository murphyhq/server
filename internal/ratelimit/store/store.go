package store

import (
	"context"
	"errors"
	"strings"

	"github.com/murphyhq/server/internal/ratelimit"
	"github.com/murphyhq/server/internal/ratelimit/store/redis"
)

type Store interface {
	ratelimit.Counter
	Ping(ctx context.Context) error
	Close() error
}

func Open(ctx context.Context, url string) (Store, error) {
	scheme, _, _ := strings.Cut(url, "://")
	switch scheme {
	case "redis", "rediss":
		return redis.Open(ctx, url)
	default:
		return nil, errors.New("unsupported store")
	}
}
