package lifecycle

import (
	"context"
	"sync"
	"time"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
)

func Every(interval time.Duration, task func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if err := task(ctx); err != nil {
					logger.Warn(ctx, "periodic task failed", telemetry.WithError(err))
				}
			}
		}
	}
}

func Together(ctx context.Context, tasks ...func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for _, task := range tasks {
		wg.Go(func() {
			if err := task(ctx); err != nil {
				once.Do(func() { first = err })
				cancel()
			}
		})
	}
	wg.Wait()
	return first
}
