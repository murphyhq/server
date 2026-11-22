package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/murphyhq/server/internal/aws"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/nats"
)

type Topology map[string][]string

func Database(ctx context.Context, adminURL, name string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return fmt.Errorf("look up database %s: %w", name, err)
	}
	if exists {
		return nil
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}
	return nil
}

func NATS(ctx context.Context, serverURL, stream string, topology Topology, version int) error {
	conn, js, err := nats.Open(nats.Config{URL: serverURL})
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       stream,
		Subjects:   []string{stream + ".>"},
		Retention:  jetstream.InterestPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     7 * 24 * time.Hour,
		Duplicates: 2 * time.Minute,
		Metadata:   map[string]string{messaging.TopologyVersionKey: strconv.Itoa(version)},
	})
	if err != nil {
		return fmt.Errorf("create stream %s: %w", stream, err)
	}
	for group, topics := range topology {
		subjects := make([]string, len(topics))
		for i, topic := range topics {
			subjects[i] = stream + "." + topic
		}
		_, err := js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
			Durable:        group,
			FilterSubjects: subjects,
			AckPolicy:      jetstream.AckExplicitPolicy,
			AckWait:        30 * time.Second,
			MaxAckPending:  64,
			Metadata:       map[string]string{messaging.TopologyVersionKey: strconv.Itoa(version)},
		})
		if err != nil {
			return fmt.Errorf("create consumer %s: %w", group, err)
		}
	}
	return nil
}

type SNSResources struct {
	TopicARN string
	Queues   map[string]string
}

func SNS(ctx context.Context, prefix string, topology Topology, version int) (SNSResources, error) {
	cfg, err := aws.Config(ctx)
	if err != nil {
		return SNSResources{}, err
	}
	snsClient, sqsClient := sns.NewFromConfig(cfg), sqs.NewFromConfig(cfg)
	tag := strconv.Itoa(version)

	topic, err := snsClient.CreateTopic(ctx, &sns.CreateTopicInput{Name: sdkaws.String(prefix + "-events")})
	if err != nil {
		return SNSResources{}, fmt.Errorf("create topic %s-events: %w", prefix, err)
	}
	if _, err := snsClient.TagResource(ctx, &sns.TagResourceInput{
		ResourceArn: topic.TopicArn,
		Tags:        []snstypes.Tag{{Key: sdkaws.String(messaging.TopologyVersionKey), Value: &tag}},
	}); err != nil {
		return SNSResources{}, fmt.Errorf("tag topic %s-events: %w", prefix, err)
	}

	resources := SNSResources{TopicARN: *topic.TopicArn, Queues: map[string]string{}}
	for group, topics := range topology {
		queueURL, err := queue(ctx, snsClient, sqsClient, *topic.TopicArn, prefix+"-"+group, topics, tag)
		if err != nil {
			return SNSResources{}, err
		}
		resources.Queues[group] = queueURL
	}
	return resources, nil
}

func queue(ctx context.Context, snsClient *sns.Client, sqsClient *sqs.Client, topicARN, name string, topics []string, version string) (string, error) {
	created, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: &name})
	if err != nil {
		return "", fmt.Errorf("create queue %s: %w", name, err)
	}
	if _, err := sqsClient.TagQueue(ctx, &sqs.TagQueueInput{
		QueueUrl: created.QueueUrl,
		Tags:     map[string]string{messaging.TopologyVersionKey: version},
	}); err != nil {
		return "", fmt.Errorf("tag queue %s: %w", name, err)
	}
	attributes, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       created.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", fmt.Errorf("read queue %s: %w", name, err)
	}
	queueARN := attributes.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

	policy, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect":    "Allow",
			"Principal": map[string]string{"Service": "sns.amazonaws.com"},
			"Action":    "sqs:SendMessage",
			"Resource":  queueARN,
			"Condition": map[string]any{"ArnEquals": map[string]string{"aws:SourceArn": topicARN}},
		}},
	})
	if err != nil {
		return "", err
	}
	if _, err := sqsClient.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl:   created.QueueUrl,
		Attributes: map[string]string{"Policy": string(policy)},
	}); err != nil {
		return "", fmt.Errorf("allow the topic to send to queue %s: %w", name, err)
	}

	filter, err := json.Marshal(map[string][]string{"topic": topics})
	if err != nil {
		return "", err
	}
	subscription, err := subscriptionOf(ctx, snsClient, topicARN, queueARN)
	if err != nil {
		return "", err
	}
	if subscription == "" {
		_, err = snsClient.Subscribe(ctx, &sns.SubscribeInput{
			TopicArn:   &topicARN,
			Protocol:   sdkaws.String("sqs"),
			Endpoint:   &queueARN,
			Attributes: map[string]string{"RawMessageDelivery": "true", "FilterPolicy": string(filter)},
		})
	} else {
		_, err = snsClient.SetSubscriptionAttributes(ctx, &sns.SetSubscriptionAttributesInput{
			SubscriptionArn: &subscription,
			AttributeName:   sdkaws.String("FilterPolicy"),
			AttributeValue:  sdkaws.String(string(filter)),
		})
	}
	if err != nil {
		return "", fmt.Errorf("subscribe queue %s: %w", name, err)
	}
	return *created.QueueUrl, nil
}

func subscriptionOf(ctx context.Context, snsClient *sns.Client, topicARN, queueARN string) (string, error) {
	pages := sns.NewListSubscriptionsByTopicPaginator(snsClient, &sns.ListSubscriptionsByTopicInput{TopicArn: &topicARN})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list subscriptions: %w", err)
		}
		for _, subscription := range page.Subscriptions {
			if sdkaws.ToString(subscription.Endpoint) == queueARN {
				return sdkaws.ToString(subscription.SubscriptionArn), nil
			}
		}
	}
	return "", nil
}

func DropNATS(ctx context.Context, serverURL, stream string) error {
	conn, js, err := nats.Open(nats.Config{URL: serverURL})
	if err != nil {
		return err
	}
	defer conn.Close()
	return js.DeleteStream(ctx, stream)
}

func DropSNS(ctx context.Context, prefix string, groups []string) error {
	cfg, err := aws.Config(ctx)
	if err != nil {
		return err
	}
	snsClient, sqsClient := sns.NewFromConfig(cfg), sqs.NewFromConfig(cfg)

	var errs []error
	for _, group := range groups {
		queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: sdkaws.String(prefix + "-" + group)})
		if err == nil {
			_, err = sqsClient.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: queue.QueueUrl})
		}
		errs = append(errs, err)
	}
	topics, err := snsClient.ListTopics(ctx, &sns.ListTopicsInput{})
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, topic := range topics.Topics {
		if strings.HasSuffix(sdkaws.ToString(topic.TopicArn), ":"+prefix+"-events") {
			_, err := snsClient.DeleteTopic(ctx, &sns.DeleteTopicInput{TopicArn: topic.TopicArn})
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
