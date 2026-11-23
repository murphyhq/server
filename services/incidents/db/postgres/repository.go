package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
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
		Service:    "incidents",
		Migrations: migrations,
		Tx:         func(tx pgx.Tx) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.Pool())}, nil
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

func (r *Repository) GetIncident(ctx context.Context, id string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIncident(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("incident", err)
	}
	return toIncident(row), nil
}

func (r *Repository) ListIncidents(ctx context.Context, filter incidents.Filter, before string, limit int) (_ []*murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	texts := make([]string, len(filter.Statuses))
	for i, status := range filter.Statuses {
		texts[i] = status.Text()
	}
	rows, err := r.queries.ListIncidents(ctx, ListIncidentsParams{Before: before, Statuses: texts, ServiceIds: filter.ServiceIDs, MaxRows: int32(limit)})
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
			AlertId:    database.TextValue(row.AlertID),
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

func (r *Repository) OpenInstances(ctx context.Context, incidentID string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	instances, err := r.queries.OpenInstances(ctx, &incidentID)
	if err != nil {
		return nil, murphypostgres.Error("alert", err)
	}
	return instances, nil
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
		ResolvedAt: database.Timestamp(row.ResolvedAt),
	}
}
