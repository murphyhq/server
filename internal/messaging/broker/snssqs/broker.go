package snssqs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
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
	typeAttribute = "type"
	maxInFlight   = 64
	maxBatch      = 10
)

type Topic struct {
	sns *sns.Client
	arn string
}

func OpenTopic(ctx context.Context, rawURL string) (*Topic, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "" || !strings.HasPrefix(u.Path, "/arn:") {
		return nil, errors.New("an SNS topic URL names the topic ARN, as in awssns:///arn:aws:sns:eu-west-1:123456789012:murphy-events")
	}
	cfg, err := aws.Config(ctx)
	if err != nil {
		return nil, err
	}
	t := &Topic{sns: sns.NewFromConfig(cfg), arn: strings.TrimPrefix(u.Path, "/")}

	tags, err := t.sns.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{ResourceArn: &t.arn})
	if err != nil {
		return nil, fmt.Errorf("topic %s: %w", t.arn, err)
	}
	version := ""
	for _, tag := range tags.Tags {
		if sdkaws.ToString(tag.Key) == messaging.TopologyVersionKey {
			version = sdkaws.ToString(tag.Value)
		}
	}
	if err := messaging.CheckTopologyVersion("topic "+t.arn, version); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Topic) System() string {
	return "aws_sns"
}

func (t *Topic) Publish(ctx context.Context, eventType, _ string, data []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = t.sns.Publish(ctx, &sns.PublishInput{
		TopicArn: &t.arn,
		Message:  sdkaws.String(base64.StdEncoding.EncodeToString(data)),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			typeAttribute: {DataType: sdkaws.String("String"), StringValue: &eventType},
		},
	})
	if err != nil {
		return fmt.Errorf("publish %s: %w", eventType, err)
	}
	return nil
}

func (t *Topic) Ping(ctx context.Context) error {
	if _, err := t.sns.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{TopicArn: &t.arn}); err != nil {
		return fmt.Errorf("topic %s: %w", t.arn, err)
	}
	return nil
}

func (t *Topic) Close() error {
	return nil
}

type Subscription struct {
	sqs      *sqs.Client
	queueURL string
}

func OpenSubscription(ctx context.Context, rawURL string) (*Subscription, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || strings.Count(strings.Trim(u.Path, "/"), "/") != 1 {
		return nil, errors.New("an SQS subscription URL names the queue, as in awssqs://sqs.eu-west-1.amazonaws.com/123456789012/murphy-engine")
	}
	cfg, err := aws.Config(ctx)
	if err != nil {
		return nil, err
	}
	s := &Subscription{sqs: sqs.NewFromConfig(cfg), queueURL: "https://" + u.Host + u.Path}

	tags, err := s.sqs.ListQueueTags(ctx, &sqs.ListQueueTagsInput{QueueUrl: &s.queueURL})
	if err != nil {
		return nil, fmt.Errorf("queue %s: %w", s.queueURL, err)
	}
	if err := messaging.CheckTopologyVersion("queue "+s.queueURL, tags.Tags[messaging.TopologyVersionKey]); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Subscription) System() string {
	return "aws_sqs"
}

func (s *Subscription) Consume(ctx context.Context, handle func(context.Context, messaging.Delivery) error) (messaging.Consumer, error) {
	pollCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	c := &consumer{stop: stop, pool: messaging.NewPool(maxInFlight)}
	c.polling.Go(func() {
		s.poll(pollCtx, context.WithoutCancel(ctx), handle, c.pool)
	})
	return c, nil
}

func (s *Subscription) Ping(ctx context.Context) error {
	if _, err := s.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: &s.queueURL}); err != nil {
		return fmt.Errorf("queue %s: %w", s.queueURL, err)
	}
	return nil
}

func (s *Subscription) Close() error {
	return nil
}

func (s *Subscription) poll(ctx, handlerCtx context.Context, handle func(context.Context, messaging.Delivery) error, pool *messaging.Pool) {
	failures := 0
	for pool.Acquire(ctx) {
		reserved := 1 + pool.Reserve(maxBatch-1)
		out, err := s.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:                    &s.queueURL,
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
			logger.Warn(handlerCtx, "receiving messages failed", telemetry.WithField("queue", s.queueURL), telemetry.WithError(err))
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
				s.settle(handlerCtx, message, handle)
			})
		}
	}
}

func (s *Subscription) settle(ctx context.Context, message sqstypes.Message, handle func(context.Context, messaging.Delivery) error) {
	attempt, _ := strconv.Atoi(message.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)])
	data, err := base64.StdEncoding.DecodeString(sdkaws.ToString(message.Body))
	if err == nil {
		err = handle(ctx, messaging.Delivery{Data: data, Attempt: max(attempt, 1)})
	} else {
		err = messaging.Reject(fmt.Errorf("decode message body: %w", err))
	}

	if err == nil || messaging.Rejected(err) {
		_, err = s.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: &s.queueURL, ReceiptHandle: message.ReceiptHandle})
	} else {
		_, err = s.sqs.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          &s.queueURL,
			ReceiptHandle:     message.ReceiptHandle,
			VisibilityTimeout: int32(messaging.RetryAfter(attempt).Seconds()) + 1,
		})
	}
	if err != nil {
		logger.Warn(ctx, "message settlement failed", telemetry.WithField("queue", s.queueURL), telemetry.WithError(err))
	}
}

type consumer struct {
	stop    context.CancelFunc
	polling sync.WaitGroup
	pool    *messaging.Pool
}

func (c *consumer) Stop() {
	c.stop()
	c.polling.Wait()
	c.pool.Wait()
}
