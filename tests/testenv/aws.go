//go:build integration

package testenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/aws"
	"github.com/murphyhq/server/internal/messaging/brokertest"
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
			ctx := t.Context()
			cfg, err := aws.Config(ctx)
			require.NoError(t, err)
			snsClient, sqsClient := sns.NewFromConfig(cfg), sqs.NewFromConfig(cfg)
			prefix := name()

			topic, err := snsClient.CreateTopic(ctx, &sns.CreateTopicInput{Name: sdkaws.String(prefix + "-events")})
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = snsClient.DeleteTopic(context.Background(), &sns.DeleteTopicInput{TopicArn: topic.TopicArn})
			})

			for group, topics := range topology {
				queue, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: sdkaws.String(prefix + "-" + group)})
				require.NoError(t, err)
				t.Cleanup(func() {
					_, _ = sqsClient.DeleteQueue(context.Background(), &sqs.DeleteQueueInput{QueueUrl: queue.QueueUrl})
				})
				attributes, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
					QueueUrl:       queue.QueueUrl,
					AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
				})
				require.NoError(t, err)
				queueARN := attributes.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

				policy, err := json.Marshal(map[string]any{
					"Version": "2012-10-17",
					"Statement": []map[string]any{{
						"Effect":    "Allow",
						"Principal": map[string]string{"Service": "sns.amazonaws.com"},
						"Action":    "sqs:SendMessage",
						"Resource":  queueARN,
						"Condition": map[string]any{"ArnEquals": map[string]string{"aws:SourceArn": *topic.TopicArn}},
					}},
				})
				require.NoError(t, err)
				_, err = sqsClient.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
					QueueUrl:   queue.QueueUrl,
					Attributes: map[string]string{"Policy": string(policy)},
				})
				require.NoError(t, err)

				filter, err := json.Marshal(map[string][]string{"topic": topics})
				require.NoError(t, err)
				_, err = snsClient.Subscribe(ctx, &sns.SubscribeInput{
					TopicArn: topic.TopicArn,
					Protocol: sdkaws.String("sqs"),
					Endpoint: &queueARN,
					Attributes: map[string]string{
						"RawMessageDelivery": "true",
						"FilterPolicy":       string(filter),
					},
				})
				require.NoError(t, err)
			}
			return fmt.Sprintf("sqs://%s", prefix)
		},
		Missing: func(t *testing.T) string {
			return "sqs://" + name()
		},
	}
}
