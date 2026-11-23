package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"

	"github.com/murphyhq/server/internal/metrics"
)

var commands = metrics.NewPerScopeCounter("redis.commands", "Redis commands")

type countingHook struct{}

func (countingHook) DialHook(next goredis.DialHook) goredis.DialHook {
	return next
}

func (countingHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		commands.Add(ctx)
		return next(ctx, cmd)
	}
}

func (countingHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for range cmds {
			commands.Add(ctx)
		}
		return next(ctx, cmds)
	}
}
