package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	pool    *pgxpool.Pool
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	pool, err := murphypostgres.Open(ctx, murphypostgres.Config{URL: url})
	if err != nil {
		return nil, err
	}
	return &Repository{pool: pool, queries: New(pool)}, nil
}

func (r *Repository) Migrate(ctx context.Context) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load incidents migrations: %w", err)
	}
	return murphypostgres.Migrate(ctx, r.pool, murphypostgres.Migrations{Service: "incidents", Files: files})
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.FindOpenIncident(ctx, FindOpenIncidentParams{ServiceID: serviceID, DedupKey: dedupKey})
	if err != nil {
		return nil, storeError("incident", err)
	}
	return toIncident(row), nil
}

func (r *Repository) RecordAlert(ctx context.Context, outcome *incidents.AlertOutcome) (recorded bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	labels, err := json.Marshal(outcome.Alert.GetLabels())
	if err != nil {
		return false, fmt.Errorf("encode labels: %w", err)
	}
	now := time.Now().UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := r.queries.WithTx(tx)

	marked, err := q.MarkHandled(ctx, MarkHandledParams{ID: outcome.MessageID, CreatedAt: now})
	if err != nil {
		return false, storeError("message", err)
	}
	if marked == 0 {
		return false, nil
	}

	if incident := outcome.Opened; incident != nil {
		number, err := q.CreateIncident(ctx, CreateIncidentParams{
			ID:        incident.GetId(),
			ServiceID: incident.GetServiceId(),
			DedupKey:  incident.GetDedupKey(),
			Title:     incident.GetTitle(),
			Status:    incident.GetStatus().Text(),
			Urgency:   incident.GetUrgency().Text(),
			Version:   incident.GetVersion(),
			CreatedAt: incident.GetCreatedAt().AsTime(),
			UpdatedAt: incident.GetUpdatedAt().AsTime(),
		})
		if err != nil {
			return false, storeError("incident", err)
		}
		incident.Number = number
	}
	if incident := outcome.Joined; incident != nil {
		rows, err := q.TouchIncident(ctx, TouchIncidentParams{
			ID:        incident.GetId(),
			Version:   incident.GetVersion(),
			UpdatedAt: incident.GetUpdatedAt().AsTime(),
		})
		if err != nil {
			return false, storeError("incident", err)
		}
		if rows == 0 {
			return false, murphy.ErrConflict
		}
	}
	if incident := outcome.Resolved; incident != nil {
		rows, err := q.UpdateIncidentStatus(ctx, UpdateIncidentStatusParams{
			ID:         incident.GetId(),
			Version:    incident.GetVersion(),
			Status:     incident.GetStatus().Text(),
			ResolvedAt: fromTimestamp(incident.GetResolvedAt()),
			UpdatedAt:  incident.GetUpdatedAt().AsTime(),
		})
		if err != nil {
			return false, storeError("incident", err)
		}
		if rows == 0 {
			return false, murphy.ErrConflict
		}
	}

	alert := outcome.Alert
	if err := q.CreateAlert(ctx, CreateAlertParams{
		ID:            alert.GetId(),
		IncidentID:    optional(outcome.IncidentID),
		IntegrationID: alert.GetIntegrationId(),
		ServiceID:     alert.GetServiceId(),
		DedupKey:      alert.GetDedupKey(),
		Status:        alert.GetStatus().Text(),
		Summary:       alert.GetSummary(),
		Labels:        labels,
		ReceivedAt:    alert.GetReceivedAt().AsTime(),
		CreatedAt:     now,
	}); err != nil {
		return false, storeError("alert", err)
	}
	for _, entry := range outcome.Timeline {
		if err := q.CreateTimelineEntry(ctx, CreateTimelineEntryParams{
			ID:         entry.GetId(),
			IncidentID: entry.GetIncidentId(),
			Type:       entry.GetType().Text(),
			AlertID:    optional(entry.GetAlertId()),
			Summary:    entry.GetSummary(),
			CreatedAt:  entry.GetAt().AsTime(),
		}); err != nil {
			return false, storeError("timeline entry", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, storeError("incident", err)
	}
	return true, nil
}

func (r *Repository) GetIncident(ctx context.Context, id string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIncident(ctx, id)
	if err != nil {
		return nil, storeError("incident", err)
	}
	return toIncident(row), nil
}

func (r *Repository) ListIncidents(ctx context.Context, statuses []murphy.IncidentStatus, before string, limit int) (_ []*murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	texts := make([]string, len(statuses))
	for i, status := range statuses {
		texts[i] = status.Text()
	}
	rows, err := r.queries.ListIncidents(ctx, ListIncidentsParams{Before: before, Statuses: texts, MaxRows: int32(limit)})
	if err != nil {
		return nil, storeError("incident", err)
	}
	items := make([]*murphy.Incident, len(rows))
	for i, row := range rows {
		items[i] = toIncident(row)
	}
	return items, nil
}

func (r *Repository) ListTimeline(ctx context.Context, incidentID string) (_ []*murphy.TimelineEntry, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListTimeline(ctx, incidentID)
	if err != nil {
		return nil, storeError("timeline entry", err)
	}
	entries := make([]*murphy.TimelineEntry, len(rows))
	for i, row := range rows {
		entries[i] = &murphy.TimelineEntry{
			Id:         row.ID,
			IncidentId: row.IncidentID,
			Type:       murphy.TimelineEntryTypeFromText(row.Type),
			AlertId:    value(row.AlertID),
			Summary:    row.Summary,
			At:         timestamppb.New(row.CreatedAt),
		}
	}
	return entries, nil
}

func (r *Repository) LatestFiringAt(ctx context.Context, incidentID string) (_ time.Time, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	latest, err := r.queries.LatestFiringAt(ctx, &incidentID)
	if err != nil {
		return time.Time{}, storeError("alert", err)
	}
	return latest, nil
}

func (r *Repository) PurgeHandledMessages(ctx context.Context, before time.Time) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.queries.PurgeHandledMessages(ctx, before)
}

func toIncident(row Incident) *murphy.Incident {
	return &murphy.Incident{
		Id:         row.ID,
		Number:     row.Number,
		ServiceId:  row.ServiceID,
		DedupKey:   row.DedupKey,
		Title:      row.Title,
		Status:     murphy.IncidentStatusFromText(row.Status),
		Urgency:    murphy.UrgencyFromText(row.Urgency),
		Version:    row.Version,
		CreatedAt:  timestamppb.New(row.CreatedAt),
		UpdatedAt:  timestamppb.New(row.UpdatedAt),
		ResolvedAt: toTimestamp(row.ResolvedAt),
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func fromTimestamp(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func toTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func storeError(entity string, err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return &murphy.NotFoundError{Entity: entity}
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return murphy.ErrConflict
	}
	return err
}
