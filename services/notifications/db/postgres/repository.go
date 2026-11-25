package postgres

import (
	"context"
	"embed"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jackc/pgx/v5"

	"github.com/murphyhq/server"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	*murphypostgres.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphypostgres.OpenDatabase(ctx, murphypostgres.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "notifications",
		Migrations: migrations,
		Tx:         func(tx pgx.Tx) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.Pool())}, nil
}

func (r *Repository) RecipientsOf(ctx context.Context, incidentID string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	users, err := r.queries.RecipientsOf(ctx, incidentID)
	if err != nil {
		return nil, murphypostgres.Error("page recipient", err)
	}
	return users, nil
}

func (r *Repository) DeleteMessages(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("scheduled message", r.queries.DeleteMessages(ctx, ids))
}

func (r *Repository) PurgeHandledMessages(ctx context.Context, before time.Time) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	purged, err := r.queries.PurgeHandledMessages(ctx, before.UTC())
	if err != nil {
		return 0, murphypostgres.Error("message", err)
	}
	return purged, nil
}

func (r *Repository) RecordOutcome(ctx context.Context, messageID string, notification *murphy.Notification, at time.Time) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	params := RecordOutcomeParams{MessageID: messageID, At: at, ID: notification.GetId(), Status: "failed"}
	if failure := notification.GetFailure(); failure != "" {
		params.Failure = &failure
	}
	if sent := notification.GetSentAt(); sent != nil {
		sentAt := sent.AsTime()
		params.Status, params.SentAt = "sent", &sentAt
	}
	handled, err := r.queries.RecordOutcome(ctx, params)
	if err != nil {
		return false, murphypostgres.Error("notification", err)
	}
	return handled == 1, nil
}

func (r *Repository) RecordPage(ctx context.Context, change *notifications.Change, at time.Time) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	page := change.Page
	params := RecordPageParams{
		MessageID: change.MessageID, At: at, PageID: page.GetId(), IncidentID: page.GetIncident().GetId(),
		EscalationLevel: page.GetEscalationLevel(), EscalationRound: page.GetEscalationRound(), CreatedAt: page.GetCreatedAt().AsTime(),
	}
	for _, recipient := range page.GetReached() {
		params.RecipientIds = append(params.RecipientIds, murphy.NewID())
		params.RecipientUserIds = append(params.RecipientUserIds, recipient.GetUserId())
		params.RecipientViaTypes = append(params.RecipientViaTypes, recipient.GetVia().GetType().Text())
		params.RecipientViaIds = append(params.RecipientViaIds, recipient.GetVia().GetId())
	}
	for _, notification := range change.Notifications {
		params.NotificationIds = append(params.NotificationIds, notification.GetId())
		params.NotificationUserIds = append(params.NotificationUserIds, notification.GetUserId())
		params.ContactMethodIds = append(params.ContactMethodIds, notification.GetContactMethod().GetId())
		params.Channels = append(params.Channels, notification.GetContactMethod().GetType().Text())
	}
	for _, message := range change.Messages {
		params.MessageIds = append(params.MessageIds, message.ID)
		params.DueAts = append(params.DueAts, message.DueAt.UTC())
		params.Envelopes = append(params.Envelopes, message.Envelope)
	}
	handled, err := r.queries.RecordPage(ctx, params)
	if err != nil {
		return false, murphypostgres.Error("page", err)
	}
	return handled == 1, nil
}

func (r *Repository) GetEmailProvider(ctx context.Context) (_ *murphy.EmailProvider, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetEmailProvider(ctx)
	if err != nil {
		return nil, murphypostgres.Error("email provider", err)
	}
	return &murphy.EmailProvider{
		Type: murphy.EmailProviderTypeFromText(row.Type), From: row.FromAddress, Host: row.Host, Port: row.Port,
		Username: row.Username, Secret: row.Secret, UpdatedAt: timestamppb.New(row.UpdatedAt),
	}, nil
}

func (r *Repository) SetEmailProvider(ctx context.Context, provider *murphy.EmailProvider) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("email provider", r.queries.SetEmailProvider(ctx, SetEmailProviderParams{
		Type: provider.GetType().Text(), FromAddress: provider.GetFrom(), Host: provider.GetHost(), Port: provider.GetPort(),
		Username: provider.GetUsername(), Secret: provider.GetSecret(), UpdatedAt: provider.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) DeleteEmailProvider(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("email provider", r.queries.DeleteEmailProvider(ctx))
}
