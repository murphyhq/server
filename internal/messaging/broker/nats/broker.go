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

const maxInFlight = 64

type Broker struct {
	conn     *gonats.Conn
	js       jetstream.JetStream
	stream   string
	consumer string
}

func Open(ctx context.Context, rawURL string) (*Broker, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("not a valid NATS URL")
	}
	stream := strings.Trim(u.Path, "/")
	if stream == "" {
		return nil, errors.New("the NATS URL must name the stream in its path, as in nats://host:4222/events")
	}

	conn, js, err := nats.Open(nats.Config{URL: rawURL})
	if err != nil {
		return nil, err
	}
	b := &Broker{conn: conn, js: js, stream: stream, consumer: u.Query().Get("consumer")}
	info, err := js.Stream(ctx, stream)
	if err == nil {
		err = messaging.CheckTopologyVersion("stream "+stream, info.CachedInfo().Config.Metadata[messaging.TopologyVersionKey])
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("stream %s: %w", stream, err)
	}
	return b, nil
}

func (b *Broker) System() string {
	return "nats"
}

func (b *Broker) Publish(ctx context.Context, topic, id string, data []byte) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if _, err := b.js.Publish(ctx, b.stream+"."+topic, data, jetstream.WithMsgID(id)); err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, handle func(context.Context, messaging.Delivery) error) (messaging.Subscription, error) {
	if b.consumer == "" {
		return nil, errors.New("the NATS URL must name the consumer, as in nats://host:4222/events?consumer=engine")
	}
	consumer, err := b.js.Consumer(ctx, b.stream, b.consumer)
	if err == nil {
		err = messaging.CheckTopologyVersion("consumer "+b.consumer, consumer.CachedInfo().Config.Metadata[messaging.TopologyVersionKey])
	}
	if err != nil {
		return nil, fmt.Errorf("consumer %s: %w", b.consumer, err)
	}

	handlerCtx := context.WithoutCancel(ctx)
	sub := &subscription{pool: messaging.NewPool(maxInFlight)}
	sub.consumeContext, err = consumer.Consume(func(msg jetstream.Msg) {
		sub.pool.Acquire(handlerCtx)
		sub.pool.Go(func() {
			b.settle(handlerCtx, msg, handle)
		})
	})
	if err != nil {
		return nil, fmt.Errorf("consume %s: %w", b.consumer, err)
	}
	return sub, nil
}

func (b *Broker) settle(ctx context.Context, msg jetstream.Msg, handle func(context.Context, messaging.Delivery) error) {
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
		logger.Warn(ctx, "message settlement failed", telemetry.WithField("consumer", b.consumer), telemetry.WithError(err))
	}
}

func (b *Broker) Ping(ctx context.Context) error {
	if _, err := b.js.Stream(ctx, b.stream); err != nil {
		return fmt.Errorf("stream %s: %w", b.stream, err)
	}
	if b.consumer != "" {
		if _, err := b.js.Consumer(ctx, b.stream, b.consumer); err != nil {
			return fmt.Errorf("consumer %s: %w", b.consumer, err)
		}
	}
	return nil
}

func (b *Broker) Close() error {
	return b.conn.Drain()
}

type subscription struct {
	consumeContext jetstream.ConsumeContext
	pool           *messaging.Pool
}

func (s *subscription) Stop() {
	s.consumeContext.Drain()
	<-s.consumeContext.Closed()
	s.pool.Wait()
}
