//go:build integration

package testenv

import (
	"context"
	"maps"
	"net/url"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/aws"
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
		Provision: func(t *testing.T, topology brokertest.Topology, version int) brokertest.Endpoints {
			t.Helper()
			prefix := name()
			cfg, err := aws.Config(t.Context())
			require.NoError(t, err)
			resources, err := provision.SNS(t.Context(), cfg, prefix, provision.Topology(topology), version)
			t.Cleanup(func() {
				_ = provision.DropSNS(context.Background(), cfg, prefix, slices.Collect(maps.Keys(topology)))
			})
			require.NoError(t, err)

			endpoints := brokertest.Endpoints{Publisher: sqsURL(resources.TopicARN, ""), Consumers: map[string]string{}}
			for group, queue := range resources.Queues {
				endpoints.Consumers[group] = sqsURL(resources.TopicARN, queue)
			}
			return endpoints
		},
		Missing: func(t *testing.T) string {
			return sqsURL("arn:aws:sns:us-east-1:000000000000:"+name(), "")
		},
	}
}

func sqsURL(topicARN, queueURL string) string {
	query := url.Values{"topic": {topicARN}}
	if queueURL != "" {
		query.Set("queue", queueURL)
	}
	return "sqs://?" + query.Encode()
}
