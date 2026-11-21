package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/murphyhq/server/internal/idempotency"
	"github.com/murphyhq/server/internal/redis"
	"github.com/murphyhq/server/internal/tracer"
)

const prefix = "murphy:idempotency:"

type Store struct {
	client *goredis.Client
}

func Open(url string) (*Store, error) {
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

func (s *Store) Reserve(ctx context.Context, key string, record idempotency.Record, ttl time.Duration) (_ *idempotency.Record, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode idempotency record: %w", err)
	}
	for range 3 {
		err := s.client.SetArgs(ctx, prefix+key, data, goredis.SetArgs{Mode: "NX", TTL: ttl}).Err()
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, goredis.Nil) {
			return nil, fmt.Errorf("reserve idempotency key: %w", err)
		}

		raw, err := s.client.Get(ctx, prefix+key).Bytes()
		if errors.Is(err, goredis.Nil) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read idempotency key: %w", err)
		}
		var existing idempotency.Record
		if err := json.Unmarshal(raw, &existing); err != nil {
			return nil, fmt.Errorf("decode idempotency record: %w", err)
		}
		return &existing, nil
	}
	return nil, errors.New("reserve idempotency key: it kept expiring while being read")
}

func (s *Store) Complete(ctx context.Context, key string, record idempotency.Record) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode idempotency record: %w", err)
	}
	err = s.client.SetArgs(ctx, prefix+key, data, goredis.SetArgs{Mode: "XX", KeepTTL: true}).Err()
	if errors.Is(err, goredis.Nil) {
		return errors.New("complete idempotency key: it expired before the request finished")
	}
	if err != nil {
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	return nil
}

func (s *Store) Release(ctx context.Context, key string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.client.Del(ctx, prefix+key).Err(); err != nil {
		return fmt.Errorf("release idempotency key: %w", err)
	}
	return nil
}
