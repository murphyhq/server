package nats

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/nats"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const defaultStream = "events"

var streamName = regexp.MustCompile(`^[a-z0-9_]+$`)

type Broker struct {
	conn   *gonats.Conn
	js     jetstream.JetStream
	stream string
}

func Open(ctx context.Context, rawURL string) (*Broker, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("not a valid NATS URL")
	}
	stream := strings.Trim(u.Path, "/")
	if stream == "" {
		stream = defaultStream
	}
	if !streamName.MatchString(stream) {
		return nil, errors.New("the stream name in the NATS URL path must be lower case letters, digits and underscores")
	}

	conn, js, err := nats.Open(nats.Config{URL: rawURL})
	if err != nil {
		return nil, err
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       stream,
		Subjects:   []string{stream + ".>"},
		Retention:  jetstream.InterestPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     7 * 24 * time.Hour,
		Duplicates: 2 * time.Minute,
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create stream %s: %w", stream, err)
	}
	return &Broker{conn: conn, js: js, stream: stream}, nil
}

func (b *Broker) System() string {
	return "nats"
}

func (b *Broker) Publish(ctx context.Context, topic, _, id string, data []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if _, err := b.js.Publish(ctx, b.subject(topic), data, jetstream.WithMsgID(id)); err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, group string, topics []string, handle func(context.Context, messaging.Delivery) error) (messaging.Subscription, error) {
	subjects := make([]string, len(topics))
	for i, topic := range topics {
		subjects[i] = b.subject(topic)
	}
	consumer, err := b.js.CreateOrUpdateConsumer(ctx, b.stream, jetstream.ConsumerConfig{
		Durable:        group,
		FilterSubjects: subjects,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        30 * time.Second,
		MaxAckPending:  1,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer %s: %w", group, err)
	}

	handlerCtx := context.WithoutCancel(ctx)
	consumeContext, err := consumer.Consume(func(msg jetstream.Msg) {
		attempt := 1
		if metadata, err := msg.Metadata(); err == nil {
			attempt = int(metadata.NumDelivered)
		}
		err := handle(handlerCtx, messaging.Delivery{Data: msg.Data(), Attempt: attempt})
		switch {
		case err == nil:
			err = msg.DoubleAck(handlerCtx)
		case messaging.Rejected(err):
			err = msg.Term()
		default:
			err = msg.NakWithDelay(messaging.RetryAfter(attempt))
		}
		if err != nil {
			logger.Warn(handlerCtx, "message settlement failed", telemetry.WithField("consumer.group", group), telemetry.WithError(err))
		}
	})
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", group, err)
	}
	return subscription{consumeContext}, nil
}

func (b *Broker) Ping(ctx context.Context) error {
	_, err := b.js.Stream(ctx, b.stream)
	return err
}

func (b *Broker) Close() error {
	return b.conn.Drain()
}

func (b *Broker) subject(topic string) string {
	return b.stream + "." + topic
}

type subscription struct {
	consumeContext jetstream.ConsumeContext
}

func (s subscription) Stop() {
	s.consumeContext.Drain()
	<-s.consumeContext.Closed()
}
