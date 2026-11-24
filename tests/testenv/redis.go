//go:build integration

package testenv

import "os"

func RedisURL() string {
	if url := os.Getenv("MURPHY_TEST_REDIS_URL"); url != "" {
		return url
	}
	return "redis://localhost:6379/0"
}

func IdentityProviderURL() string {
	if url := os.Getenv("MURPHY_TEST_IDENTITY_PROVIDER_URL"); url != "" {
		return url
	}
	return "http://localhost:5556/dex"
}
