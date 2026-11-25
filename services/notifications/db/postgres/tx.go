package postgres

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
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

func (t *Tx) CreatePage(ctx context.Context, page *murphy.Page) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	at := page.GetCreatedAt().AsTime()
	if err := t.queries.CreatePage(ctx, CreatePageParams{
		ID: page.GetId(), IncidentID: page.GetIncident().GetId(), EscalationLevel: page.GetEscalationLevel(), EscalationRound: page.GetEscalationRound(), CreatedAt: at,
	}); err != nil {
		return murphypostgres.Error("page", err)
	}
	if len(page.GetReached()) == 0 {
		return nil
	}
	params := CreatePageRecipientsParams{PageID: page.GetId(), IncidentID: page.GetIncident().GetId(), CreatedAt: at}
	for _, recipient := range page.GetReached() {
		params.Ids = append(params.Ids, murphy.NewID())
		params.UserIds = append(params.UserIds, recipient.GetUserId())
		params.ViaTypes = append(params.ViaTypes, recipient.GetVia().GetType().Text())
		params.ViaIds = append(params.ViaIds, recipient.GetVia().GetId())
	}
	return murphypostgres.Error("page recipient", t.queries.CreatePageRecipients(ctx, params))
}

func (t *Tx) CreateNotifications(ctx context.Context, created []*murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	first := created[0]
	subject, subjectID := notifications.SubjectOf(first)
	params := CreateNotificationsParams{
		Subject: subject, SubjectID: subjectID, Reason: first.GetReason().Text(), CreatedAt: first.GetCreatedAt().AsTime(),
	}
	if first.GetPageId() != "" {
		page := first.GetPageId()
		params.PageID = &page
	}
	for _, notification := range created {
		params.Ids = append(params.Ids, notification.GetId())
		params.UserIds = append(params.UserIds, notification.GetUserId())
		params.ContactMethodIds = append(params.ContactMethodIds, notification.GetContactMethod().GetId())
		params.Channels = append(params.Channels, notification.GetContactMethod().GetType().Text())
	}
	return murphypostgres.Error("notification", t.queries.CreateNotifications(ctx, params))
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
	return murphypostgres.Error("notification", t.queries.UpdateNotificationOutcome(ctx, params))
}

func (t *Tx) CreateScheduledMessages(ctx context.Context, messages []*notifications.ScheduledMessage, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	params := CreateScheduledMessagesParams{CreatedAt: at}
	for _, message := range messages {
		params.Ids = append(params.Ids, message.ID)
		params.DueAts = append(params.DueAts, message.DueAt.UTC())
		params.Envelopes = append(params.Envelopes, message.Envelope)
		params.WaitingIncidentIds = append(params.WaitingIncidentIds, message.Waiting.GetIncident().GetId())
		params.WaitingNotificationIds = append(params.WaitingNotificationIds, message.Waiting.GetId())
	}
	return murphypostgres.Error("scheduled message", t.queries.CreateScheduledMessages(ctx, params))
}

func (t *Tx) CancelWaitingNotifications(ctx context.Context, incidentID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("notification", t.queries.CancelWaitingNotifications(ctx, CancelWaitingNotificationsParams{IncidentID: &incidentID, At: at}))
}

func (t *Tx) DeleteWaitingMessages(ctx context.Context, incidentID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("scheduled message", t.queries.DeleteWaitingMessages(ctx, DeleteWaitingMessagesParams{IncidentID: &incidentID, At: &at}))
}

func (t *Tx) LockDueMessages(ctx context.Context, now time.Time, limit int) (_ []*notifications.ScheduledMessage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.LockDueMessages(ctx, LockDueMessagesParams{Now: now.UTC(), ClaimsExpireBy: &now, MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("scheduled message", err)
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
	return murphypostgres.Error("scheduled message", t.queries.ClaimMessages(ctx, ClaimMessagesParams{ClaimedUntil: &until, Ids: ids}))
}
