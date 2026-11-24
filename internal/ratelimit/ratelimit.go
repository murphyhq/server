package ratelimit

import (
	"context"
	"time"
)

type Counter interface {
	Hit(ctx context.Context, key string, window time.Duration) (int64, error)
	Count(ctx context.Context, key string) (int64, error)
	Reset(ctx context.Context, key string) error
}
