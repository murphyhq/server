package main

import (
	"context"
	"fmt"
	"os"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
)

type settings struct {
	APIURL  string `env:"SEED_API_URL" default:"http://localhost:8080"`
	Dogfood dogfoodSettings
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "seed failed", telemetry.WithError(err))
		os.Exit(1)
	}
	logger.Info(ctx, "seeded")
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	api, err := client.NewClientWithResponses(s.APIURL)
	if err != nil {
		return fmt.Errorf("create API client: %w", err)
	}
	teams, users, err := seedPeople(ctx, api)
	if err != nil {
		return err
	}
	schedules, err := seedSchedules(ctx, api, teams, users)
	if err != nil {
		return err
	}
	policies, err := seedPolicies(ctx, api, teams, users, schedules)
	if err != nil {
		return err
	}
	if err := seedCatalog(ctx, api, policies); err != nil {
		return err
	}
	return seedDogfood(ctx, api, policies["platform"], s.Dogfood)
}

func byName[T any](list func(cursor *string) ([]T, string, error), key func(T) (string, string)) (map[string]string, error) {
	ids := map[string]string{}
	var cursor *string
	for {
		items, next, err := list(cursor)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			name, id := key(item)
			ids[name] = id
		}
		if next == "" {
			return ids, nil
		}
		cursor = &next
	}
}

type response interface {
	StatusCode() int
	Status() string
}

func expect(r response, err error, want int, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if r.StatusCode() != want {
		return fmt.Errorf("%s: %s", what, r.Status())
	}
	return nil
}
