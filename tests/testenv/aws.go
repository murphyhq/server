//go:build integration

package testenv

import (
	"context"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/tests/provision"
)

func SQS(t *testing.T) brokertest.Environment {
	for key, value := range map[string]string{
		"AWS_ENDPOINT_URL":      "http://localhost:4566",
		"AWS_REGION":            "us-east-1",
		"AWS_ACCESS_KEY_ID":     "test",
		"AWS_SECRET_ACCESS_KEY": "test",
	} {
		if os.Getenv(key) == "" {
			t.Setenv(key, value)
		}
	}

	return brokertest.Environment{
		Provision: func(t *testing.T, topology brokertest.Topology) string {
			t.Helper()
			prefix := name()
			t.Cleanup(func() {
				_ = provision.DropSNS(context.Background(), prefix, slices.Collect(maps.Keys(topology)))
			})
			require.NoError(t, provision.SNS(t.Context(), prefix, provision.Topology(topology)))
			return "sqs://" + prefix
		},
		Missing: func(t *testing.T) string {
			return "sqs://" + name()
		},
	}
}
