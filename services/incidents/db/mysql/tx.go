package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
)

type Tx struct {
	tx      murphymysql.DBTX
	queries *Queries
}

func (t *Tx) MarkHandled(ctx context.Context, messageID string, at time.Time) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.MarkHandled(ctx, MarkHandledParams{ID: messageID, CreatedAt: at})
	if err != nil {
		return false, murphymysql.Error("message", err)
	}
	return rows == 1, nil
}

func (t *Tx) CreateIncident(ctx context.Context, incident *murphy.Incident) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	result, err := t.queries.CreateIncident(ctx, CreateIncidentParams{
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
		return murphymysql.Error("incident", err)
	}
	number, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read the incident number: %w", err)
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

		AcknowledgedAt:      database.Time(incident.GetAcknowledgedAt()),
		EscalationLevel:     incident.GetEscalationLevel(),
		EscalationRound:     incident.GetEscalationRound(),
		EscalationExhausted: incident.GetEscalationExhausted(),
	})
	if err != nil {
		return murphymysql.Error("incident", err)
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
	return murphymysql.Error("alert", t.queries.CreateAlert(ctx, CreateAlertParams{
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

func (t *Tx) CreateTimelineEntry(ctx context.Context, entry *murphy.TimelineEntry) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	details, err := proto.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode timeline entry: %w", err)
	}
	return murphymysql.Error("timeline entry", t.queries.CreateTimelineEntry(ctx, CreateTimelineEntryParams{
		ID:         entry.GetId(),
		IncidentID: entry.GetIncidentId(),
		Type:       entry.GetType().Text(),
		Summary:    entry.GetSummary(),
		Details:    details,
		CreatedAt:  entry.GetAt().AsTime(),
	}))
}

func (t *Tx) CreateScheduledMessages(ctx context.Context, messages []*incidents.ScheduledMessage, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateScheduledMessages", Table: "scheduled_messages", Columns: []string{"id", "incident_id", "kind", "due_at", "envelope", "created_at"}}
	for _, message := range messages {
		rows.Values = append(rows.Values, []any{message.ID, message.IncidentID, string(message.Kind), message.DueAt.UTC(), message.Envelope, at})
	}
	return murphymysql.Error("scheduled message", murphymysql.Insert(ctx, t.tx, rows))
}

func (t *Tx) DeleteTimers(ctx context.Context, incidentID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("scheduled message", t.queries.DeleteTimers(ctx, incidentID))
}

func (t *Tx) LockDueMessages(ctx context.Context, now time.Time, limit int) (_ []*incidents.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.LockDueMessages(ctx, LockDueMessagesParams{Now: now.UTC(), ClaimsExpireBy: &now, Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("scheduled message", err)
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
	return murphymysql.Error("scheduled message", t.queries.ClaimMessages(ctx, ClaimMessagesParams{ClaimedUntil: &until, Ids: ids}))
}
