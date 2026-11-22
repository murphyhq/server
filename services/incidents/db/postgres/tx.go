package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
)

type Tx struct {
	queries *Queries
}

func (t *Tx) MarkHandled(ctx context.Context, messageID string, at time.Time) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.MarkHandled(ctx, MarkHandledParams{ID: messageID, CreatedAt: at})
	if err != nil {
		return false, murphypostgres.Error("message", err)
	}
	return rows == 1, nil
}

func (t *Tx) CreateIncident(ctx context.Context, incident *murphy.Incident) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	number, err := t.queries.CreateIncident(ctx, CreateIncidentParams{
		ID:         incident.GetId(),
		ServiceID:  incident.GetServiceId(),
		DedupKey:   incident.GetDedupKey(),
		Title:      incident.GetTitle(),
		Status:     incident.GetStatus().Text(),
		Urgency:    incident.GetUrgency().Text(),
		Version:    incident.GetVersion(),
		CreatedAt:  incident.GetCreatedAt().AsTime(),
		UpdatedAt:  incident.GetUpdatedAt().AsTime(),
		ResolvedAt: database.Time(incident.GetResolvedAt()),
	})
	if err != nil {
		return murphypostgres.Error("incident", err)
	}
	incident.Number = number
	return nil
}

func (t *Tx) UpdateIncident(ctx context.Context, incident *murphy.Incident) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.UpdateIncident(ctx, UpdateIncidentParams{
		ID:         incident.GetId(),
		Version:    incident.GetVersion(),
		Status:     incident.GetStatus().Text(),
		ResolvedAt: database.Time(incident.GetResolvedAt()),
		UpdatedAt:  incident.GetUpdatedAt().AsTime(),
	})
	if err != nil {
		return murphypostgres.Error("incident", err)
	}
	if rows == 0 {
		return murphy.ErrConflict
	}
	return nil
}

func (t *Tx) CreateAlert(ctx context.Context, alert *murphy.Alert, incidentID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	labels, err := json.Marshal(alert.GetLabels())
	if err != nil {
		return fmt.Errorf("encode labels: %w", err)
	}
	return murphypostgres.Error("alert", t.queries.CreateAlert(ctx, CreateAlertParams{
		ID:            alert.GetId(),
		IncidentID:    database.Text(incidentID),
		IntegrationID: alert.GetIntegrationId(),
		ServiceID:     alert.GetServiceId(),
		DedupKey:      alert.GetDedupKey(),
		Status:        alert.GetStatus().Text(),
		Summary:       alert.GetSummary(),
		Labels:        labels,
		ReceivedAt:    alert.GetReceivedAt().AsTime(),
		CreatedAt:     time.Now().UTC(),
	}))
}

func (t *Tx) CreateTimelineEntry(ctx context.Context, entry *murphy.TimelineEntry) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("timeline entry", t.queries.CreateTimelineEntry(ctx, CreateTimelineEntryParams{
		ID:         entry.GetId(),
		IncidentID: entry.GetIncidentId(),
		Type:       entry.GetType().Text(),
		AlertID:    database.Text(entry.GetAlertId()),
		Summary:    entry.GetSummary(),
		CreatedAt:  entry.GetAt().AsTime(),
	}))
}
