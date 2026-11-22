package messaging

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/metrics"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Delivery struct {
	Data    []byte
	Attempt int
}

type Topic interface {
	System() string
	Publish(ctx context.Context, topic, id string, data []byte) error
	Ping(ctx context.Context) error
	Close() error
}

type Subscription interface {
	System() string
	Consume(ctx context.Context, handle func(context.Context, Delivery) error) (Consumer, error)
	Ping(ctx context.Context) error
	Close() error
}

type Consumer interface {
	Stop()
}

var errRejected = errors.New("message rejected")

func Reject(err error) error {
	return fmt.Errorf("%w: %w", errRejected, err)
}

func Rejected(err error) bool {
	return errors.Is(err, errRejected)
}

func RetryAfter(attempt int) time.Duration {
	delay := time.Second << min(max(attempt-1, 0), 6)
	delay = min(delay, time.Minute)
	return delay/2 + rand.N(delay/2+1)
}

type Handler func(ctx context.Context, envelope *murphy.Envelope) error

type Handlers map[string]Handler

var event = (&murphy.Envelope{}).ProtoReflect().Descriptor().Oneofs().ByName("event")

func TopicOf(envelope *murphy.Envelope) string {
	field := envelope.ProtoReflect().WhichOneof(event)
	if field == nil {
		return ""
	}
	return topicOf(field)
}

func Topics() []string {
	topics := make([]string, event.Fields().Len())
	for i := range topics {
		topics[i] = topicOf(event.Fields().Get(i))
	}
	return topics
}

func topicOf(field protoreflect.FieldDescriptor) string {
	return string(field.ParentFile().Package()) + "." + string(field.Name())
}

type Publisher struct {
	topic Topic
}

func NewPublisher(topic Topic) *Publisher {
	return &Publisher{topic: topic}
}

func (p *Publisher) Publish(ctx context.Context, envelope *murphy.Envelope) (err error) {
	topic := TopicOf(envelope)
	if topic == "" || envelope.GetId() == "" {
		return errors.New("publish message: it needs an ID and an event")
	}

	ctx, span := tracer.Span(ctx, tracer.Producer())
	defer span.End(&err)
	ctx = span.Set(ctx, attributes(p.topic.System(), topic, envelope.GetId())...)

	message := proto.CloneOf(envelope)
	message.TraceContext = tracer.Inject(ctx)
	data, err := proto.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if err := p.topic.Publish(ctx, topic, envelope.GetId(), data); err != nil {
		return fmt.Errorf("publish message: %w", err)
	}
	published.Add(ctx, 1, metrics.Dimension{Name: "topic", Value: topic})
	return nil
}

func Consume(ctx context.Context, subscription Subscription, group string, handlers Handlers) (Consumer, error) {
	if len(handlers) == 0 {
		return nil, errors.New("consume: no handlers")
	}
	known := map[string]bool{}
	for _, topic := range Topics() {
		known[topic] = true
	}
	for topic := range handlers {
		if !known[topic] {
			return nil, fmt.Errorf("consume: unknown topic %s", topic)
		}
	}

	consumer, err := subscription.Consume(ctx, func(ctx context.Context, delivery Delivery) error {
		return process(ctx, subscription.System(), group, handlers, delivery)
	})
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", group, err)
	}
	return consumer, nil
}

func process(ctx context.Context, system, group string, handlers Handlers, delivery Delivery) (err error) {
	started := time.Now()
	envelope := &murphy.Envelope{}
	if err := proto.Unmarshal(delivery.Data, envelope); err != nil {
		logger.Error(ctx, "message rejected", telemetry.WithField("consumer.group", group), telemetry.WithError(err))
		return Reject(fmt.Errorf("decode message: %w", err))
	}
	topic := TopicOf(envelope)

	ctx, span := tracer.Span(ctx, tracer.Consumer(), tracer.ContinuedFrom(envelope.GetTraceContext()))
	defer span.End(&err)
	ctx = span.Set(ctx, append(attributes(system, topic, envelope.GetId()),
		telemetry.Standard("messaging.consumer.group.name", group),
		telemetry.WithField("message.attempt", delivery.Attempt),
	)...)

	if handle, ok := handlers[topic]; ok {
		err = handle(ctx, envelope)
	} else {
		err = Reject(fmt.Errorf("%s has no handler for %s", group, topic))
	}
	if err != nil && errors.Is(err, murphy.ErrInvalid) {
		err = Reject(err)
	}

	outcome, log := "handled", logger.Info
	switch {
	case Rejected(err):
		outcome, log = "rejected", logger.Error
	case err != nil:
		outcome, log = "retried", logger.Warn
	}
	elapsed := time.Since(started)
	fields := []telemetry.Field{
		telemetry.WithField("message.outcome", outcome),
		telemetry.WithField("duration_ms", elapsed.Milliseconds()),
	}
	if err != nil {
		fields = append(fields, telemetry.WithError(err))
	}
	log(ctx, "message handled", fields...)

	dimensions := []metrics.Dimension{{Name: "topic", Value: topic}, {Name: "consumer.group", Value: group}, {Name: "outcome", Value: outcome}}
	processed.Add(ctx, 1, dimensions...)
	duration.Record(ctx, elapsed.Seconds(), dimensions...)
	return err
}

func attributes(system, topic, id string) []telemetry.Field {
	return []telemetry.Field{
		telemetry.Standard("messaging.system", system),
		telemetry.Standard("messaging.destination.name", topic),
		telemetry.Standard("messaging.message.id", id),
	}
}

var published, processed, duration = instruments()

func instruments() (*metrics.Counter, *metrics.Counter, *metrics.Histogram) {
	published, err := metrics.NewCounter("messages.published", "Messages published to the broker")
	if err != nil {
		panic(err)
	}
	processed, err := metrics.NewCounter("messages.processed", "Messages handled by a consumer, by outcome")
	if err != nil {
		panic(err)
	}
	duration, err := metrics.NewHistogram("message.process.duration", "Time to handle a message", "s")
	if err != nil {
		panic(err)
	}
	return published, processed, duration
}
