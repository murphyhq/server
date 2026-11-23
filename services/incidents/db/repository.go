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
}

type Engine[T Tx] interface {
	FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (*murphy.Incident, error)
	GetIncident(ctx context.Context, id string) (*murphy.Incident, error)
	ListIncidents(ctx context.Context, filter incidents.Filter, before string, limit int) ([]*murphy.Incident, error)
	ListTimeline(ctx context.Context, incidentID string) ([]*murphy.TimelineEntry, error)
	LatestFiringAt(ctx context.Context, incidentID string) (time.Time, error)
	OpenInstances(ctx context.Context, incidentID string) ([]string, error)
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var _ incidents.Repository = (*Repository[Tx])(nil)

func (r *Repository[T]) RecordAlert(ctx context.Context, outcome *incidents.AlertOutcome) (recorded bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	err = r.Transaction(ctx, func(tx T) error {
		first, err := tx.MarkHandled(ctx, outcome.MessageID, time.Now().UTC())
		if err != nil || !first {
			return err
		}
		if incident := outcome.Created; incident != nil {
			if err := tx.CreateIncident(ctx, incident); err != nil {
				return err
			}
		}
		if incident := outcome.Updated; incident != nil {
			if err := tx.UpdateIncident(ctx, incident); err != nil {
				return err
			}
		}
		if err := tx.CreateAlert(ctx, outcome.Alert, outcome.IncidentID); err != nil {
			return err
		}
		for _, entry := range outcome.Timeline {
			if err := tx.CreateTimelineEntry(ctx, entry); err != nil {
				return err
			}
		}
		recorded = true
		return nil
	})
	return recorded, err
}
