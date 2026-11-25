package db

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
)

type Tx interface {
	MarkHandled(ctx context.Context, messageID string, at time.Time) (first bool, err error)
	CreatePage(ctx context.Context, page *murphy.Page) error
	CreateNotifications(ctx context.Context, notifications []*murphy.Notification) error
	UpdateOutcome(ctx context.Context, notification *murphy.Notification, at time.Time) error
	CreateScheduledMessages(ctx context.Context, messages []*notifications.ScheduledMessage, at time.Time) error
	CancelWaitingNotifications(ctx context.Context, incidentID string, at time.Time) error
	DeleteWaitingMessages(ctx context.Context, incidentID string, at time.Time) error
	LockDueMessages(ctx context.Context, now time.Time, limit int) ([]*notifications.ScheduledMessage, error)
	ClaimMessages(ctx context.Context, ids []string, until time.Time) error
}

type Engine[T Tx] interface {
	RecipientsOf(ctx context.Context, incidentID string) ([]string, error)
	GetTestPage(ctx context.Context, id, userID string) (*notifications.TestPage, error)
	TestsSince(ctx context.Context, userID string, since time.Time) (int, error)
	DeleteMessages(ctx context.Context, ids []string) error
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
	GetEmailProvider(ctx context.Context) (*murphy.EmailProvider, error)
	SetEmailProvider(ctx context.Context, provider *murphy.EmailProvider) error
	DeleteEmailProvider(ctx context.Context) error
	RecordEmailProviderCheck(ctx context.Context, provider *murphy.EmailProvider) error
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var _ notifications.Repository = (*Repository[Tx])(nil)

type outcomeRecorder interface {
	RecordOutcome(ctx context.Context, messageID string, notification *murphy.Notification, at time.Time) (bool, error)
}

type pageRecorder interface {
	RecordPage(ctx context.Context, change *notifications.Change, at time.Time) (bool, error)
}

func (r *Repository[T]) Record(ctx context.Context, change *notifications.Change) (recorded bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := time.Now().UTC()
	onlyOutcome := change.MessageID != "" && change.Outcome != nil && change.Page == nil && len(change.Notifications) == 0 && len(change.Messages) == 0
	if recorder, ok := any(r.Engine).(outcomeRecorder); ok && onlyOutcome {
		return recorder.RecordOutcome(ctx, change.MessageID, change.Outcome, now)
	}
	onlyPage := change.MessageID != "" && change.Page != nil && change.Outcome == nil
	if recorder, ok := any(r.Engine).(pageRecorder); ok && onlyPage {
		return recorder.RecordPage(ctx, change, now)
	}
	err = r.Transaction(ctx, func(tx T) error {
		if change.MessageID != "" {
			first, err := tx.MarkHandled(ctx, change.MessageID, now)
			if err != nil || !first {
				return err
			}
		}
		if change.StopWaiting != "" {
			if err := tx.CancelWaitingNotifications(ctx, change.StopWaiting, now); err != nil {
				return err
			}
			if err := tx.DeleteWaitingMessages(ctx, change.StopWaiting, now); err != nil {
				return err
			}
		}
		if change.Page != nil {
			if err := tx.CreatePage(ctx, change.Page); err != nil {
				return err
			}
		}
		if len(change.Notifications) > 0 {
			if err := tx.CreateNotifications(ctx, change.Notifications); err != nil {
				return err
			}
		}
		if change.Outcome != nil {
			if err := tx.UpdateOutcome(ctx, change.Outcome, now); err != nil {
				return err
			}
		}
		if len(change.Messages) > 0 {
			if err := tx.CreateScheduledMessages(ctx, change.Messages, now); err != nil {
				return err
			}
		}
		recorded = true
		return nil
	})
	return recorded, err
}

func (r *Repository[T]) ClaimDueMessages(ctx context.Context, now time.Time, limit int, lease time.Duration) (_ []*notifications.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	var claimed []*notifications.ScheduledMessage
	err = r.Transaction(ctx, func(tx T) error {
		due, err := tx.LockDueMessages(ctx, now, limit)
		if err != nil || len(due) == 0 {
			return err
		}
		ids := make([]string, len(due))
		for i, message := range due {
			ids[i] = message.ID
		}
		if err := tx.ClaimMessages(ctx, ids, now.Add(lease)); err != nil {
			return err
		}
		claimed = due
		return nil
	})
	return claimed, err
}
