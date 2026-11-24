package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
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

		EscalationPolicyID:      incident.GetEscalationPolicyId(),
		EscalationPolicyVersion: incident.GetEscalationPolicyVersion(),
		EscalationLevel:         incident.GetEscalationLevel(),
		EscalationRound:         incident.GetEscalationRound(),
		EscalationExhausted:     incident.GetEscalationExhausted(),
		AcknowledgedAt:          database.Time(incident.GetAcknowledgedAt()),
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

		PreviousVersion: incident.GetVersion() - 1,

		AcknowledgedAt:      database.Time(incident.GetAcknowledgedAt()),
		EscalationLevel:     incident.GetEscalationLevel(),
		EscalationRound:     incident.GetEscalationRound(),
		EscalationExhausted: incident.GetEscalationExhausted(),
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
	annotations, err := json.Marshal(alert.GetAnnotations())
	if err != nil {
		return fmt.Errorf("encode annotations: %w", err)
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
		Source:        murphy.Or(alert.GetSource(), murphy.IntegrationType_INTEGRATION_TYPE_GENERIC).Text(),
		Instance:      database.Text(alert.GetInstance()),
		Annotations:   annotations,
		ReceivedAt:    alert.GetReceivedAt().AsTime(),
		CreatedAt:     time.Now().UTC(),
	}))
}

func (t *Tx) CreateTimelineEntries(ctx context.Context, entries []*murphy.TimelineEntry) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(entries) == 0 {
		return nil
	}
	var params CreateTimelineEntriesParams
	for _, entry := range entries {
		details, err := proto.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode timeline entry: %w", err)
		}
		params.Ids = append(params.Ids, entry.GetId())
		params.IncidentIds = append(params.IncidentIds, entry.GetIncidentId())
		params.Types = append(params.Types, entry.GetType().Text())
		params.Summaries = append(params.Summaries, entry.GetSummary())
		params.Details = append(params.Details, details)
		params.CreatedAts = append(params.CreatedAts, entry.GetAt().AsTime())
	}
	return murphypostgres.Error("timeline entry", t.queries.CreateTimelineEntries(ctx, params))
}

func (t *Tx) CreateScheduledMessages(ctx context.Context, messages []*incidents.ScheduledMessage, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(messages) == 0 {
		return nil
	}
	params := CreateScheduledMessagesParams{CreatedAt: at}
	for _, message := range messages {
		params.Ids = append(params.Ids, message.ID)
		params.IncidentIds = append(params.IncidentIds, message.IncidentID)
		params.Kinds = append(params.Kinds, string(message.Kind))
		params.DueAts = append(params.DueAts, message.DueAt.UTC())
		params.Envelopes = append(params.Envelopes, message.Envelope)
	}
	return murphypostgres.Error("scheduled message", t.queries.CreateScheduledMessages(ctx, params))
}

func (t *Tx) DeleteTimers(ctx context.Context, incidentID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("scheduled message", t.queries.DeleteTimers(ctx, incidentID))
}

func (t *Tx) LockDueMessages(ctx context.Context, now time.Time, limit int) (_ []*incidents.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.LockDueMessages(ctx, LockDueMessagesParams{Now: now.UTC(), ClaimsExpireBy: &now, MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("scheduled message", err)
	}
	messages := make([]*incidents.ScheduledMessage, len(rows))
	for i, row := range rows {
		messages[i] = &incidents.ScheduledMessage{
			ID: row.ID, IncidentID: row.IncidentID, Kind: incidents.MessageKind(row.Kind), DueAt: row.DueAt, Envelope: row.Envelope,
		}
	}
	return messages, nil
}

func (t *Tx) ClaimMessages(ctx context.Context, ids []string, until time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	until = until.UTC()
	return murphypostgres.Error("scheduled message", t.queries.ClaimMessages(ctx, ClaimMessagesParams{ClaimedUntil: &until, Ids: ids}))
}

func (t *Tx) CreatePages(ctx context.Context, entries []*murphy.TimelineEntry, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	params := CreatePagesParams{CreatedAt: at}
	for _, entry := range entries {
		for _, page := range entry.GetEscalation().GetPaged() {
			params.Ids = append(params.Ids, murphy.NewID())
			params.IncidentIds = append(params.IncidentIds, entry.GetIncidentId())
			params.UserIds = append(params.UserIds, page.GetUserId())
		}
	}
	if len(params.Ids) == 0 {
		return nil
	}
	return murphypostgres.Error("page", t.queries.CreatePages(ctx, params))
}
