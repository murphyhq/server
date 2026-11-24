package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/murphyhq/server/internal/redis"
	"github.com/murphyhq/server/internal/tracer"
)

const prefix = "murphy:ratelimit:"

type Store struct {
	client *goredis.Client
}

func Open(_ context.Context, url string) (*Store, error) {
	client, err := redis.Open(redis.Config{URL: url})
	if err != nil {
		return nil, err
	}
	return &Store{client: client}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) Hit(ctx context.Context, key string, window time.Duration) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	pipe := s.client.TxPipeline()
	count := pipe.Incr(ctx, prefix+key)
	pipe.ExpireNX(ctx, prefix+key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return count.Val(), nil
}

func (s *Store) Count(ctx context.Context, key string) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	count, err := s.client.Get(ctx, prefix+key).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	return count, err
}

func (s *Store) Reset(ctx context.Context, key string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return s.client.Del(ctx, prefix+key).Err()
}
