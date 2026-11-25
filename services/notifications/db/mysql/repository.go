package mysql

import (
	"context"
	"embed"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	*murphymysql.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphymysql.OpenDatabase(ctx, murphymysql.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "notifications",
		Migrations: migrations,
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{tx: tx, queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

func (r *Repository) RecipientsOf(ctx context.Context, incidentID string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	users, err := r.queries.RecipientsOf(ctx, incidentID)
	if err != nil {
		return nil, murphymysql.Error("page recipient", err)
	}
	return users, nil
}

func (r *Repository) DeleteMessages(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return nil
	}
	return murphymysql.Error("scheduled message", r.queries.DeleteMessages(ctx, ids))
}

func (r *Repository) PurgeHandledMessages(ctx context.Context, before time.Time) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	purged, err := r.queries.PurgeHandledMessages(ctx, before.UTC())
	if err != nil {
		return 0, murphymysql.Error("message", err)
	}
	return purged, nil
}

func (r *Repository) GetEmailProvider(ctx context.Context) (_ *murphy.EmailProvider, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetEmailProvider(ctx)
	if err != nil {
		return nil, murphymysql.Error("email provider", err)
	}
	return &murphy.EmailProvider{
		Type: murphy.EmailProviderTypeFromText(row.Type), From: row.FromAddress, Host: row.Host, Port: row.Port,
		Username: row.Username, Secret: row.Secret, UpdatedAt: timestamppb.New(row.UpdatedAt),
		Status: murphy.EmailProviderStatusFromText(row.Status), Problem: row.Problem, CheckedAt: optionalTimestamp(row.CheckedAt),
	}, nil
}

func (r *Repository) SetEmailProvider(ctx context.Context, provider *murphy.EmailProvider) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("email provider", r.queries.SetEmailProvider(ctx, SetEmailProviderParams{
		Type: provider.GetType().Text(), FromAddress: provider.GetFrom(), Host: provider.GetHost(), Port: provider.GetPort(),
		Username: provider.GetUsername(), Secret: provider.GetSecret(), UpdatedAt: provider.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) RecordEmailProviderCheck(ctx context.Context, provider *murphy.EmailProvider) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	checked := provider.GetCheckedAt().AsTime()
	_, err = r.queries.RecordEmailProviderCheck(ctx, RecordEmailProviderCheckParams{
		Status: provider.GetStatus().Text(), Problem: provider.GetProblem(), CheckedAt: &checked, UpdatedAt: provider.GetUpdatedAt().AsTime(),
	})
	return murphymysql.Error("email provider", err)
}

func optionalTimestamp(at *time.Time) *timestamppb.Timestamp {
	if at == nil {
		return nil
	}
	return timestamppb.New(*at)
}

func (r *Repository) DeleteEmailProvider(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("email provider", r.queries.DeleteEmailProvider(ctx))
}

func (r *Repository) GetTestPage(ctx context.Context, id, userID string) (_ *notifications.TestPage, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListTestNotifications(ctx, ListTestNotificationsParams{TestID: id, UserID: userID})
	if err != nil {
		return nil, murphymysql.Error("test page", err)
	}
	if len(rows) == 0 {
		return nil, &murphy.NotFoundError{Entity: "test page"}
	}
	page := &notifications.TestPage{ID: id, UserID: userID, CreatedAt: rows[0].CreatedAt}
	for _, row := range rows {
		status := notifications.DeliveryPending
		if row.Status == "sent" || row.Status == "failed" {
			status = notifications.DeliveryStatus(row.Status)
		}
		failure := ""
		if row.Failure != nil {
			failure = *row.Failure
		}
		page.Deliveries = append(page.Deliveries, &notifications.TestDelivery{
			ContactMethodID: row.ContactMethodID, Channel: murphy.ContactMethodTypeFromText(row.Channel), Status: status, Failure: failure,
		})
	}
	return page, nil
}

func (r *Repository) TestsSince(ctx context.Context, userID string, since time.Time) (_ int, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	count, err := r.queries.CountTestsSince(ctx, CountTestsSinceParams{UserID: userID, Since: since})
	return int(count), murphymysql.Error("test page", err)
}
