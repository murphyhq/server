package nats

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/nats"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

const (
	system      = "nats"
	maxInFlight = 64
)

type Topic struct {
	conn   *gonats.Conn
	js     jetstream.JetStream
	stream string
}

func OpenTopic(ctx context.Context, rawURL string) (*Topic, error) {
	conn, js, path, err := connect(rawURL)
	if err != nil {
		return nil, err
	}
	if len(path) != 1 {
		conn.Close()
		return nil, errors.New("a NATS topic URL names the stream, as in nats://host:4222/events")
	}
	stream, err := js.Stream(ctx, path[0])
	if err == nil {
		err = messaging.CheckTopologyVersion("stream "+path[0], stream.CachedInfo().Config.Metadata[messaging.TopologyVersionKey])
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("stream %s: %w", path[0], err)
	}
	return &Topic{conn: conn, js: js, stream: path[0]}, nil
}

func (t *Topic) System() string {
	return system
}

func (t *Topic) Publish(ctx context.Context, topic, id string, data []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if _, err := t.js.Publish(ctx, t.stream+"."+topic, data, jetstream.WithMsgID(id)); err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

func (t *Topic) Ping(ctx context.Context) error {
	if _, err := t.js.Stream(ctx, t.stream); err != nil {
		return fmt.Errorf("stream %s: %w", t.stream, err)
	}
	return nil
}

func (t *Topic) Close() error {
	return t.conn.Drain()
}

type Subscription struct {
	conn     *gonats.Conn
	consumer jetstream.Consumer
	stream   string
	name     string
	js       jetstream.JetStream
}

func OpenSubscription(ctx context.Context, rawURL string) (*Subscription, error) {
	conn, js, path, err := connect(rawURL)
	if err != nil {
		return nil, err
	}
	if len(path) != 2 {
		conn.Close()
		return nil, errors.New("a NATS subscription URL names the stream and the consumer, as in nats://host:4222/events/engine")
	}
	consumer, err := js.Consumer(ctx, path[0], path[1])
	if err == nil {
		err = messaging.CheckTopologyVersion("consumer "+path[1], consumer.CachedInfo().Config.Metadata[messaging.TopologyVersionKey])
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("consumer %s on stream %s: %w", path[1], path[0], err)
	}
	return &Subscription{conn: conn, consumer: consumer, js: js, stream: path[0], name: path[1]}, nil
}

func (s *Subscription) System() string {
	return system
}

func (s *Subscription) Consume(ctx context.Context, handle func(context.Context, messaging.Delivery) error) (messaging.Consumer, error) {
	handlerCtx := context.WithoutCancel(ctx)
	c := &consumer{pool: messaging.NewPool(maxInFlight)}
	var err error
	c.consumeContext, err = s.consumer.Consume(func(msg jetstream.Msg) {
		c.pool.Acquire(handlerCtx)
		c.pool.Go(func() {
			s.settle(handlerCtx, msg, handle)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", s.name, err)
	}
	return c, nil
}

func (s *Subscription) settle(ctx context.Context, msg jetstream.Msg, handle func(context.Context, messaging.Delivery) error) {
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
		logger.Warn(ctx, "message settlement failed", telemetry.WithField("consumer", s.name), telemetry.WithError(err))
	}
}

func (s *Subscription) Ping(ctx context.Context) error {
	if _, err := s.js.Consumer(ctx, s.stream, s.name); err != nil {
		return fmt.Errorf("consumer %s: %w", s.name, err)
	}
	return nil
}

func (s *Subscription) Close() error {
	return s.conn.Drain()
}

type consumer struct {
	consumeContext jetstream.ConsumeContext
	pool           *messaging.Pool
}

func (c *consumer) Stop() {
	c.consumeContext.Drain()
	<-c.consumeContext.Closed()
	c.pool.Wait()
}

func connect(rawURL string) (*gonats.Conn, jetstream.JetStream, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, nil, errors.New("not a valid NATS URL")
	}
	var path []string
	if trimmed := strings.Trim(u.Path, "/"); trimmed != "" {
		path = strings.Split(trimmed, "/")
	}
	conn, js, err := nats.Open(nats.Config{URL: rawURL})
	if err != nil {
		return nil, nil, nil, err
	}
	return conn, js, path, nil
}
