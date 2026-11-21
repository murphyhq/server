//go:build integration

package testenv

import "os"

func RedisURL() string {
	if url := os.Getenv("MURPHY_TEST_REDIS_URL"); url != "" {
		return url
	}
	return "redis://localhost:6379/0"
}
