package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
	"google.golang.org/protobuf/proto"
)

type Provider interface {
	Send(ctx context.Context, notification *murphy.Notification) error
}

type Publisher interface {
	Publish(ctx context.Context, envelope *murphy.Envelope) error
}

const GiveUpAfter = 5 * time.Minute

type RefusedError struct {
	Reason string
}

func (e *RefusedError) Error() string {
	return "refused: " + e.Reason
}

type Sender struct {
	providers map[murphy.ContactMethodType]Provider
	publisher Publisher
}

func NewSender(publisher Publisher, providers map[murphy.ContactMethodType]Provider) *Sender {
	return &Sender{providers: providers, publisher: publisher}
}

func (s *Sender) Send(ctx context.Context, messageID string, notification *murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	channel := notification.GetContactMethod().GetType()
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", notification.GetIncident().GetId()),
		telemetry.WithField("notification.id", notification.GetId()),
		telemetry.WithField("notification.channel", channel.Text()),
	)
	defer span.End(&err)

	provider, ok := s.providers[channel]
	if !ok {
		return s.outcome(ctx, messageID, notification, murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, "Murphy can't send by "+channel.Text()+" yet")
	}
	sendErr := provider.Send(ctx, notification)
	var refused *RefusedError
	switch {
	case sendErr == nil:
		return s.outcome(ctx, messageID, notification, murphy.NotificationVerb_NOTIFICATION_VERB_SENT, "")
	case errors.As(sendErr, &refused):
		return s.outcome(ctx, messageID, notification, murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, refused.Reason)
	case time.Since(notification.GetCreatedAt().AsTime()) >= GiveUpAfter:
		return s.outcome(ctx, messageID, notification, murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, "still failing after 5 minutes: "+sendErr.Error())
	default:
		return fmt.Errorf("send notification: %w", sendErr)
	}
}

func (s *Sender) outcome(ctx context.Context, messageID string, notification *murphy.Notification, verb murphy.NotificationVerb, reason string) error {
	current := proto.CloneOf(notification)
	if verb == murphy.NotificationVerb_NOTIFICATION_VERB_SENT {
		current.SentAt = murphy.Now()
	}
	current.Failure = reason
	envelope := &murphy.Envelope{
		Id:         murphy.DerivedID(messageID, verb.String()),
		OccurredAt: murphy.Now(),
		Event:      &murphy.Envelope_Notification{Notification: &murphy.NotificationEvent{Verb: verb, Previous: notification, Current: current}},
	}
	if err := s.publisher.Publish(ctx, envelope); err != nil {
		return fmt.Errorf("publish notification outcome: %w", err)
	}
	return nil
}
