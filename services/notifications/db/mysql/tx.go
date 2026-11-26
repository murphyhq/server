package mysql

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
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

func (t *Tx) CreatePage(ctx context.Context, page *murphy.Page) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	at := page.GetCreatedAt().AsTime()
	if err := t.queries.CreatePage(ctx, CreatePageParams{
		ID: page.GetId(), IncidentID: page.GetIncident().GetId(), EscalationLevel: page.GetEscalationLevel(), EscalationRound: page.GetEscalationRound(), CreatedAt: at,
	}); err != nil {
		return murphymysql.Error("page", err)
	}
	rows := murphymysql.Rows{Name: "CreatePageRecipients", Table: "page_recipients", Columns: []string{"id", "page_id", "incident_id", "user_id", "via_type", "via_id", "created_at"}}
	for _, recipient := range page.GetReached() {
		rows.Values = append(rows.Values, []any{murphy.NewID(), page.GetId(), page.GetIncident().GetId(), recipient.GetUserId(), recipient.GetVia().GetType().Text(), recipient.GetVia().GetId(), at})
	}
	return murphymysql.Error("page recipient", murphymysql.Insert(ctx, t.tx, rows))
}

func (t *Tx) CreateNotifications(ctx context.Context, created []*murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateNotifications", Table: "notifications", Columns: []string{"id", "subject", "subject_id", "page_id", "user_id", "contact_method_id", "channel", "reason", "status", "created_at", "updated_at"}}
	for _, notification := range created {
		var page *string
		if id := notification.GetPageId(); id != "" {
			page = &id
		}
		at := notification.GetCreatedAt().AsTime()
		subject, subjectID := notifications.SubjectOf(notification)
		rows.Values = append(rows.Values, []any{
			notification.GetId(), subject, subjectID, page, notification.GetUserId(), notification.GetContactMethod().GetId(),
			notification.GetContactMethod().GetType().Text(), notification.GetReason().Text(), "created", at, at,
		})
	}
	return murphymysql.Error("notification", murphymysql.Insert(ctx, t.tx, rows))
}

func (t *Tx) UpdateOutcome(ctx context.Context, notification *murphy.Notification, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	params := UpdateNotificationOutcomeParams{ID: notification.GetId(), Status: "failed", UpdatedAt: at}
	if failure := notification.GetFailure(); failure != "" {
		params.Failure = &failure
	}
	if sent := notification.GetSentAt(); sent != nil {
		sentAt := sent.AsTime()
		params.Status, params.SentAt = "sent", &sentAt
	}
	return murphymysql.Error("notification", t.queries.UpdateNotificationOutcome(ctx, params))
}

func (t *Tx) CreateScheduledMessages(ctx context.Context, messages []*notifications.ScheduledMessage, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateScheduledMessages", Table: "scheduled_messages", Columns: []string{"id", "due_at", "envelope", "created_at", "waiting_incident_id", "waiting_notification_id"}}
	for _, message := range messages {
		rows.Values = append(rows.Values, []any{message.ID, message.DueAt.UTC(), message.Envelope, at, waitingID(message.Waiting.GetIncident().GetId()), waitingID(message.Waiting.GetId())})
	}
	return murphymysql.Error("scheduled message", murphymysql.Insert(ctx, t.tx, rows))
}

func waitingID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func (t *Tx) CancelWaitingNotifications(ctx context.Context, incidentID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("notification", t.queries.CancelWaitingNotifications(ctx, CancelWaitingNotificationsParams{IncidentID: &incidentID, At: at, ClaimsExpireBy: &at}))
}

func (t *Tx) DeleteWaitingMessages(ctx context.Context, incidentID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("scheduled message", t.queries.DeleteWaitingMessages(ctx, DeleteWaitingMessagesParams{IncidentID: &incidentID, ClaimsExpireBy: &at}))
}

func (t *Tx) LockDueMessages(ctx context.Context, now time.Time, limit int) (_ []*notifications.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.LockDueMessages(ctx, LockDueMessagesParams{Now: now.UTC(), ClaimsExpireBy: &now, Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("scheduled message", err)
	}
	messages := make([]*notifications.ScheduledMessage, len(rows))
	for i, row := range rows {
		messages[i] = &notifications.ScheduledMessage{ID: row.ID, DueAt: row.DueAt, Envelope: row.Envelope}
	}
	return messages, nil
}

func (t *Tx) ClaimMessages(ctx context.Context, ids []string, until time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	until = until.UTC()
	return murphymysql.Error("scheduled message", t.queries.ClaimMessages(ctx, ClaimMessagesParams{ClaimedUntil: &until, Ids: ids}))
}
