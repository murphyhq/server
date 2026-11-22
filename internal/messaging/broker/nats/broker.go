package nats

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/nats"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const (
	defaultStream = "events"
	maxInFlight   = 64
)

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
	b := &Broker{conn: conn, js: js, stream: stream}
	if err := b.Ping(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return b, nil
}

func (b *Broker) System() string {
	return "nats"
}

func (b *Broker) Publish(ctx context.Context, topic, id string, data []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if _, err := b.js.Publish(ctx, b.subject(topic), data, jetstream.WithMsgID(id)); err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, group string, topics []string, handle func(context.Context, messaging.Delivery) error) (messaging.Subscription, error) {
	consumer, err := b.js.Consumer(ctx, b.stream, group)
	if err != nil {
		return nil, fmt.Errorf("consumer %s on stream %s: %w", group, b.stream, err)
	}
	if err := b.check(group, consumer.CachedInfo().Config, topics); err != nil {
		return nil, err
	}

	handlerCtx := context.WithoutCancel(ctx)
	sub := &subscription{slots: make(chan struct{}, maxInFlight)}
	sub.consumeContext, err = consumer.Consume(func(msg jetstream.Msg) {
		sub.slots <- struct{}{}
		sub.handling.Go(func() {
			defer func() { <-sub.slots }()
			settle(handlerCtx, group, msg, handle)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", group, err)
	}
	return sub, nil
}

func settle(ctx context.Context, group string, msg jetstream.Msg, handle func(context.Context, messaging.Delivery) error) {
	attempt := 1
	if metadata, err := msg.Metadata(); err == nil {
		attempt = int(metadata.NumDelivered)
	}
	err := handle(ctx, messaging.Delivery{Data: msg.Data(), Attempt: attempt})
	switch {
	case err == nil:
		err = msg.DoubleAck(ctx)
	case messaging.Rejected(err):
		err = msg.Term()
	default:
		err = msg.NakWithDelay(messaging.RetryAfter(attempt))
	}
	if err != nil {
		logger.Warn(ctx, "message settlement failed", telemetry.WithField("consumer.group", group), telemetry.WithError(err))
	}
}

func (b *Broker) Ping(ctx context.Context) error {
	if _, err := b.js.Stream(ctx, b.stream); err != nil {
		return fmt.Errorf("stream %s: %w", b.stream, err)
	}
	return nil
}

func (b *Broker) Close() error {
	return b.conn.Drain()
}

func (b *Broker) check(group string, config jetstream.ConsumerConfig, topics []string) error {
	if config.AckPolicy != jetstream.AckExplicitPolicy || config.DeliverSubject != "" {
		return fmt.Errorf("consumer %s must be a pull consumer with explicit acknowledgements", group)
	}
	filters := config.FilterSubjects
	if config.FilterSubject != "" {
		filters = append(filters, config.FilterSubject)
	}
	want := make([]string, len(topics))
	for i, topic := range topics {
		want[i] = b.subject(topic)
	}
	if !sameSet(filters, want) {
		return fmt.Errorf("consumer %s filters %v, but %s handles %v", group, filters, group, want)
	}
	return nil
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func (b *Broker) subject(topic string) string {
	return b.stream + "." + topic
}

type subscription struct {
	consumeContext jetstream.ConsumeContext
	slots          chan struct{}
	handling       sync.WaitGroup
}

func (s *subscription) Stop() {
	s.consumeContext.Drain()
	<-s.consumeContext.Closed()
	s.handling.Wait()
}
