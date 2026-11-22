package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
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
		return nil, murphypostgres.Error("incident", err)
	}
	return toIncident(row), nil
}

func (r *Repository) Transaction(ctx context.Context, work func(tx *Tx) error) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	pgTx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = pgTx.Rollback(context.WithoutCancel(ctx)) }()

	if err := work(&Tx{queries: r.queries.WithTx(pgTx)}); err != nil {
		return err
	}
	if err := pgTx.Commit(ctx); err != nil {
		return murphypostgres.Error("incident", err)
	}
	return nil
}

func (r *Repository) GetIncident(ctx context.Context, id string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIncident(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("incident", err)
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
		return nil, murphypostgres.Error("incident", err)
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
		return nil, murphypostgres.Error("timeline entry", err)
	}
	entries := make([]*murphy.TimelineEntry, len(rows))
	for i, row := range rows {
		entries[i] = &murphy.TimelineEntry{
			Id:         row.ID,
			IncidentId: row.IncidentID,
			Type:       murphy.TimelineEntryTypeFromText(row.Type),
			AlertId:    murphypostgres.TextValue(row.AlertID),
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
		return time.Time{}, murphypostgres.Error("alert", err)
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
		ResolvedAt: murphypostgres.Timestamp(row.ResolvedAt),
	}
}
