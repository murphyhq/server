package sqs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
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

const topicAttribute = "topic"

var prefixName = regexp.MustCompile(`^[a-z0-9_-]+$`)

type Broker struct {
	sns      *sns.Client
	sqs      *sqs.Client
	prefix   string
	topicARN string
}

func Open(ctx context.Context, rawURL string) (*Broker, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !prefixName.MatchString(u.Host) {
		return nil, errors.New("the SQS URL must name a prefix of lower case letters, digits, hyphens and underscores, as in sqs://murphy")
	}
	cfg, err := aws.Config(ctx)
	if err != nil {
		return nil, err
	}
	b := &Broker{
		sns: sns.NewFromConfig(cfg, func(o *sns.Options) {
			o.TracerProvider, o.MeterProvider = aws.TracerProvider(), aws.MeterProvider()
		}),
		sqs: sqs.NewFromConfig(cfg, func(o *sqs.Options) {
			o.TracerProvider, o.MeterProvider = aws.TracerProvider(), aws.MeterProvider()
		}),
		prefix: u.Host,
	}
	if b.topicARN, err = b.findTopic(ctx); err != nil {
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

func (b *Broker) Subscribe(ctx context.Context, group string, topics []string, handle func(context.Context, messaging.Delivery) error) (messaging.Subscription, error) {
	queueURL, err := b.checkQueue(ctx, group, topics)
	if err != nil {
		return nil, err
	}

	pollCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	sub := &subscription{stop: stop}
	sub.polling.Go(func() {
		b.poll(pollCtx, context.WithoutCancel(ctx), group, queueURL, handle, &sub.handling)
	})
	return sub, nil
}

func (b *Broker) poll(ctx, handlerCtx context.Context, group, queueURL string, handle func(context.Context, messaging.Delivery) error, handling *sync.WaitGroup) {
	failures := 0
	for ctx.Err() == nil {
		out, err := b.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:                    &queueURL,
			MaxNumberOfMessages:         10,
			WaitTimeSeconds:             20,
			MessageSystemAttributeNames: []sqstypes.MessageSystemAttributeName{sqstypes.MessageSystemAttributeNameApproximateReceiveCount},
		})
		if err != nil {
			if ctx.Err() == nil {
				failures++
				logger.Warn(handlerCtx, "receiving messages failed", telemetry.WithField("consumer.group", group), telemetry.WithError(err))
				select {
				case <-ctx.Done():
				case <-time.After(messaging.RetryAfter(failures)):
				}
			}
			continue
		}
		failures = 0
		var batch sync.WaitGroup
		for _, message := range out.Messages {
			handling.Add(1)
			batch.Go(func() {
				defer handling.Done()
				b.settle(handlerCtx, group, queueURL, message, handle)
			})
		}
		batch.Wait()
	}
}

func (b *Broker) settle(ctx context.Context, group, queueURL string, message sqstypes.Message, handle func(context.Context, messaging.Delivery) error) {
	attempt, _ := strconv.Atoi(message.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)])
	data, err := base64.StdEncoding.DecodeString(sdkaws.ToString(message.Body))
	if err == nil {
		err = handle(ctx, messaging.Delivery{Data: data, Attempt: max(attempt, 1)})
	} else {
		err = messaging.Reject(fmt.Errorf("decode message body: %w", err))
	}

	if err == nil || messaging.Rejected(err) {
		_, err = b.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: &queueURL, ReceiptHandle: message.ReceiptHandle})
	} else {
		_, err = b.sqs.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          &queueURL,
			ReceiptHandle:     message.ReceiptHandle,
			VisibilityTimeout: int32(messaging.RetryAfter(attempt).Seconds()) + 1,
		})
	}
	if err != nil {
		logger.Warn(ctx, "message settlement failed", telemetry.WithField("consumer.group", group), telemetry.WithError(err))
	}
}

func (b *Broker) Ping(ctx context.Context) error {
	if _, err := b.sns.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{TopicArn: &b.topicARN}); err != nil {
		return fmt.Errorf("topic %s: %w", b.topicName(), err)
	}
	return nil
}

func (b *Broker) Close() error {
	return nil
}

func (b *Broker) topicName() string {
	return b.prefix + "-events"
}

func (b *Broker) queueName(group string) string {
	return b.prefix + "-" + group
}

func (b *Broker) findTopic(ctx context.Context) (string, error) {
	pages := sns.NewListTopicsPaginator(b.sns, &sns.ListTopicsInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list topics: %w", err)
		}
		for _, topic := range page.Topics {
			if strings.HasSuffix(sdkaws.ToString(topic.TopicArn), ":"+b.topicName()) {
				return sdkaws.ToString(topic.TopicArn), nil
			}
		}
	}
	return "", fmt.Errorf("topic %s does not exist", b.topicName())
}

func (b *Broker) checkQueue(ctx context.Context, group string, topics []string) (string, error) {
	name := b.queueName(group)
	queue, err := b.sqs.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: &name})
	if err != nil {
		return "", fmt.Errorf("queue %s: %w", name, err)
	}
	attributes, err := b.sqs.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       queue.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", fmt.Errorf("queue %s: %w", name, err)
	}
	queueARN := attributes.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

	subscriptionARN, err := b.findSubscription(ctx, queueARN)
	if err != nil {
		return "", err
	}
	subscription, err := b.sns.GetSubscriptionAttributes(ctx, &sns.GetSubscriptionAttributesInput{SubscriptionArn: &subscriptionARN})
	if err != nil {
		return "", fmt.Errorf("subscription of queue %s: %w", name, err)
	}
	if subscription.Attributes["RawMessageDelivery"] != "true" {
		return "", fmt.Errorf("the subscription of queue %s must use raw message delivery", name)
	}
	var policy map[string][]string
	if err := json.Unmarshal([]byte(subscription.Attributes["FilterPolicy"]), &policy); err != nil || len(policy) != 1 || !sameSet(policy[topicAttribute], topics) {
		return "", fmt.Errorf("the subscription of queue %s must filter on %s %v", name, topicAttribute, topics)
	}
	return sdkaws.ToString(queue.QueueUrl), nil
}

func (b *Broker) findSubscription(ctx context.Context, queueARN string) (string, error) {
	pages := sns.NewListSubscriptionsByTopicPaginator(b.sns, &sns.ListSubscriptionsByTopicInput{TopicArn: &b.topicARN})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("list subscriptions of topic %s: %w", b.topicName(), err)
		}
		for _, subscription := range page.Subscriptions {
			if sdkaws.ToString(subscription.Endpoint) == queueARN {
				return sdkaws.ToString(subscription.SubscriptionArn), nil
			}
		}
	}
	return "", fmt.Errorf("queue %s is not subscribed to topic %s", queueARN, b.topicName())
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

type subscription struct {
	stop     context.CancelFunc
	polling  sync.WaitGroup
	handling sync.WaitGroup
}

func (s *subscription) Stop() {
	s.stop()
	s.polling.Wait()
	s.handling.Wait()
}
