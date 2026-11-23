package db

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
)

type Tx interface {
	MarkHandled(ctx context.Context, messageID string, at time.Time) (first bool, err error)
	CreateIncident(ctx context.Context, incident *murphy.Incident) error
	UpdateIncident(ctx context.Context, incident *murphy.Incident) error
	CreateAlert(ctx context.Context, alert *murphy.Alert, incidentID string) error
	CreateTimelineEntry(ctx context.Context, entry *murphy.TimelineEntry) error
	DeleteTimers(ctx context.Context, incidentID string) error
	CreateScheduledMessages(ctx context.Context, messages []*incidents.ScheduledMessage, at time.Time) error
	LockDueMessages(ctx context.Context, now time.Time, limit int) ([]*incidents.ScheduledMessage, error)
	ClaimMessages(ctx context.Context, ids []string, until time.Time) error
}

type Engine[T Tx] interface {
	FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (*murphy.Incident, error)
	GetIncident(ctx context.Context, id string) (*murphy.Incident, error)
	ListIncidents(ctx context.Context, filter incidents.Filter, before string, limit int) ([]*murphy.Incident, error)
	ListTimeline(ctx context.Context, incidentID string) ([]*murphy.TimelineEntry, error)
	LatestFiringAt(ctx context.Context, incidentID string) (time.Time, error)
	OpenInstances(ctx context.Context, incidentID string) ([]string, error)
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
	DeleteMessages(ctx context.Context, ids []string) error
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var _ incidents.Repository = (*Repository[Tx])(nil)

func (r *Repository[T]) Record(ctx context.Context, change *incidents.Change) (recorded bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := time.Now().UTC()
	err = r.Transaction(ctx, func(tx T) error {
		if change.MessageID != "" {
			first, err := tx.MarkHandled(ctx, change.MessageID, now)
			if err != nil || !first {
				return err
			}
		}
		if incident := change.Created; incident != nil {
			if err := tx.CreateIncident(ctx, incident); err != nil {
				return err
			}
		}
		if incident := change.Updated; incident != nil {
			if err := tx.UpdateIncident(ctx, incident); err != nil {
				return err
			}
		}
		if change.Alert != nil {
			if err := tx.CreateAlert(ctx, change.Alert, change.IncidentID); err != nil {
				return err
			}
		}
		if change.CancelTimers {
			if err := tx.DeleteTimers(ctx, change.IncidentID); err != nil {
				return err
			}
		}
		for _, entry := range change.Timeline {
			if err := tx.CreateTimelineEntry(ctx, entry); err != nil {
				return err
			}
		}
		if err := tx.CreateScheduledMessages(ctx, change.Messages, now); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	return recorded, err
}

func (r *Repository[T]) ClaimDueMessages(ctx context.Context, now time.Time, limit int, lease time.Duration) (_ []*incidents.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	var claimed []*incidents.ScheduledMessage
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
