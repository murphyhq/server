package redis

import (
	"errors"
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
)

type Config struct {
	URL string
}

func Open(cfg Config) (*goredis.Client, error) {
	options, err := goredis.ParseURL(cfg.URL)
	if err != nil {
		return nil, errors.New("not a valid Redis connection string")
	}
	client := goredis.NewClient(options)
	client.AddHook(countingHook{})
	if err := errors.Join(redisotel.InstrumentTracing(client, redisotel.WithDBStatement(false)), redisotel.InstrumentMetrics(client)); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("instrument Redis client: %w", err)
	}
	return client, nil
}
