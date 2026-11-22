package sqs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/murphyhq/server/internal/aws"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const (
	topicAttribute = "topic"
	maxInFlight    = 64
	maxBatch       = 10
)

type Broker struct {
	sns      *sns.Client
	sqs      *sqs.Client
	topicARN string
	queueURL string
}

func Open(ctx context.Context, rawURL string) (*Broker, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Query().Get("topic") == "" {
		return nil, errors.New("the SQS URL must name the topic, as in sqs://?topic=<topic ARN>&queue=<queue URL>")
	}
	cfg, err := aws.Config(ctx)
	if err != nil {
		return nil, err
	}
	b := &Broker{
		sns:      sns.NewFromConfig(cfg),
		sqs:      sqs.NewFromConfig(cfg),
		topicARN: u.Query().Get("topic"),
		queueURL: u.Query().Get("queue"),
	}

	tags, err := b.sns.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{ResourceArn: &b.topicARN})
	if err != nil {
		return nil, fmt.Errorf("topic %s: %w", b.topicARN, err)
	}
	version := ""
	for _, tag := range tags.Tags {
		if sdkaws.ToString(tag.Key) == messaging.TopologyVersionKey {
			version = sdkaws.ToString(tag.Value)
		}
	}
	if err := messaging.CheckTopologyVersion("topic "+b.topicARN, version); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Broker) System() string {
	return "aws_sqs"
}

func (b *Broker) Publish(ctx context.Context, topic, _ string, data []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = b.sns.Publish(ctx, &sns.PublishInput{
		TopicArn: &b.topicARN,
		Message:  sdkaws.String(base64.StdEncoding.EncodeToString(data)),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			topicAttribute: {DataType: sdkaws.String("String"), StringValue: &topic},
		},
	})
	if err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, handle func(context.Context, messaging.Delivery) error) (messaging.Subscription, error) {
	if b.queueURL == "" {
		return nil, errors.New("the SQS URL must name the queue, as in sqs://?topic=<topic ARN>&queue=<queue URL>")
	}
	tags, err := b.sqs.ListQueueTags(ctx, &sqs.ListQueueTagsInput{QueueUrl: &b.queueURL})
	if err != nil {
		return nil, fmt.Errorf("queue %s: %w", b.queueURL, err)
	}
	if err := messaging.CheckTopologyVersion("queue "+b.queueURL, tags.Tags[messaging.TopologyVersionKey]); err != nil {
		return nil, err
	}

	pollCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	sub := &subscription{stop: stop, pool: messaging.NewPool(maxInFlight)}
	sub.polling.Go(func() {
		b.poll(pollCtx, context.WithoutCancel(ctx), handle, sub.pool)
	})
	return sub, nil
}

func (b *Broker) poll(ctx, handlerCtx context.Context, handle func(context.Context, messaging.Delivery) error, pool *messaging.Pool) {
	failures := 0
	for pool.Acquire(ctx) {
		reserved := 1 + pool.Reserve(maxBatch-1)
		out, err := b.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:                    &b.queueURL,
			MaxNumberOfMessages:         int32(reserved),
			WaitTimeSeconds:             20,
			MessageSystemAttributeNames: []sqstypes.MessageSystemAttributeName{sqstypes.MessageSystemAttributeNameApproximateReceiveCount},
		})
		var messages []sqstypes.Message
		if err == nil {
			messages = out.Messages
		}
		pool.Release(reserved - len(messages))

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			logger.Warn(handlerCtx, "receiving messages failed", telemetry.WithField("queue", b.queueURL), telemetry.WithError(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(messaging.RetryAfter(failures)):
			}
			continue
		}
		failures = 0
		for _, message := range messages {
			pool.Go(func() {
				b.settle(handlerCtx, message, handle)
			})
		}
	}
}

func (b *Broker) settle(ctx context.Context, message sqstypes.Message, handle func(context.Context, messaging.Delivery) error) {
	attempt, _ := strconv.Atoi(message.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)])
	data, err := base64.StdEncoding.DecodeString(sdkaws.ToString(message.Body))
	if err == nil {
		err = handle(ctx, messaging.Delivery{Data: data, Attempt: max(attempt, 1)})
	} else {
		err = messaging.Reject(fmt.Errorf("decode message body: %w", err))
	}

	if err == nil || messaging.Rejected(err) {
		_, err = b.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: &b.queueURL, ReceiptHandle: message.ReceiptHandle})
	} else {
		_, err = b.sqs.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          &b.queueURL,
			ReceiptHandle:     message.ReceiptHandle,
			VisibilityTimeout: int32(messaging.RetryAfter(attempt).Seconds()) + 1,
		})
	}
	if err != nil {
		logger.Warn(ctx, "message settlement failed", telemetry.WithField("queue", b.queueURL), telemetry.WithError(err))
	}
}

func (b *Broker) Ping(ctx context.Context) error {
	if _, err := b.sns.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{TopicArn: &b.topicARN}); err != nil {
		return fmt.Errorf("topic %s: %w", b.topicARN, err)
	}
	if b.queueURL != "" {
		if _, err := b.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: &b.queueURL}); err != nil {
			return fmt.Errorf("queue %s: %w", b.queueURL, err)
		}
	}
	return nil
}

func (b *Broker) Close() error {
	return nil
}

type subscription struct {
	stop    context.CancelFunc
	polling sync.WaitGroup
	pool    *messaging.Pool
}

func (s *subscription) Stop() {
	s.stop()
	s.polling.Wait()
	s.pool.Wait()
}
