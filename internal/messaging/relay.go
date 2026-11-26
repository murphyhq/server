package messaging

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/metrics"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type ScheduledMessage struct {
	ID       string
	Kind     string
	DueAt    time.Time
	Envelope []byte
}

type ScheduledMessages interface {
	ClaimDueMessages(ctx context.Context, limit int, lease time.Duration) ([]ScheduledMessage, error)
	MessagesSent(ctx context.Context, ids []string) error
}

type RelaySettings struct {
	PollInterval time.Duration `env:"POLL_INTERVAL" default:"50ms"`
	BatchSize    int           `env:"BATCH_SIZE" default:"100"`
	ClaimLease   time.Duration `env:"CLAIM_LEASE" default:"30s"`
	Publishers   int           `env:"PUBLISHERS" default:"16"`
}

func Relay(source string, messages ScheduledMessages, publisher *Publisher, settings RelaySettings) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx = telemetry.With(ctx, telemetry.WithField("relay.source", source))
		ticker := time.NewTicker(settings.PollInterval)
		defer ticker.Stop()
		for {
			full, err := relayDue(ctx, source, messages, publisher, settings)
			if err != nil {
				logger.Error(ctx, "publishing due messages failed", telemetry.WithError(err))
			}
			if full && err == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}

func relayDue(ctx context.Context, source string, messages ScheduledMessages, publisher *Publisher, settings RelaySettings) (full bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	due, err := messages.ClaimDueMessages(ctx, settings.BatchSize, settings.ClaimLease)
	if err != nil || len(due) == 0 {
		return false, err
	}
	published := make([]bool, len(due))
	var group errgroup.Group
	group.SetLimit(max(1, settings.Publishers))
	for i, message := range due {
		group.Go(func() error {
			if err := relayOne(ctx, source, publisher, message); err != nil {
				logger.Warn(ctx, "a due message will be published again once its claim expires",
					telemetry.WithField("message.id", message.ID), telemetry.WithError(err))
				return nil
			}
			published[i] = true
			return nil
		})
	}
	_ = group.Wait()
	sent := make([]string, 0, len(due))
	for i, message := range due {
		if published[i] {
			sent = append(sent, message.ID)
		}
	}
	if err := messages.MessagesSent(ctx, sent); err != nil {
		return false, err
	}
	return len(due) == settings.BatchSize, nil
}

func relayOne(ctx context.Context, source string, publisher *Publisher, message ScheduledMessage) (err error) {
	envelope := &murphy.Envelope{}
	if err := proto.Unmarshal(message.Envelope, envelope); err != nil {
		return fmt.Errorf("decode scheduled message: %w", err)
	}
	ctx, span := tracer.Span(ctx, tracer.ContinuedFrom(envelope.GetTraceContext()))
	defer span.End(&err)

	if err := publisher.Publish(ctx, envelope); err != nil {
		return err
	}
	scheduledDelay.Record(ctx, time.Since(message.DueAt).Seconds(),
		metrics.Dimension{Name: "relay.source", Value: source}, metrics.Dimension{Name: "kind", Value: message.Kind})
	return nil
}

var scheduledDelay = func() *metrics.Histogram {
	histogram, err := metrics.NewHistogram("scheduled_message.delay", "How late a scheduled message was published, after it was due", "s")
	if err != nil {
		panic(err)
	}
	return histogram
}()
