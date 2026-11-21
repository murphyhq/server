package redis

import (
	"errors"

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
	return goredis.NewClient(options), nil
}
