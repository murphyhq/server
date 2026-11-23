package mysql

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
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
		Service:    "incidents",
		Migrations: migrations,
		Tx:         func(tx *sql.Tx) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

func (r *Repository) FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.FindOpenIncident(ctx, FindOpenIncidentParams{ServiceID: serviceID, DedupKey: dedupKey})
	if err != nil {
		return nil, murphymysql.Error("incident", err)
	}
	return toIncident(GetIncidentRow(row)), nil
}

func (r *Repository) GetIncident(ctx context.Context, id string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIncident(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("incident", err)
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
	rows, err := r.queries.ListIncidents(ctx, ListIncidentsParams{
		Before:    before,
		AnyStatus: len(texts) == 0,
		Statuses:  texts,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, murphymysql.Error("incident", err)
	}
	items := make([]*murphy.Incident, len(rows))
	for i, row := range rows {
		items[i] = toIncident(GetIncidentRow(row))
	}
	return items, nil
}

func (r *Repository) ListTimeline(ctx context.Context, incidentID string) (_ []*murphy.TimelineEntry, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListTimeline(ctx, incidentID)
	if err != nil {
		return nil, murphymysql.Error("timeline entry", err)
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
	if errors.Is(err, sql.ErrNoRows) {
		return time.Unix(0, 0).UTC(), nil
	}
	if err != nil {
		return time.Time{}, murphymysql.Error("alert", err)
	}
	return latest, nil
}

func (r *Repository) OpenInstances(ctx context.Context, incidentID string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	instances, err := r.queries.OpenInstances(ctx, &incidentID)
	if err != nil {
		return nil, murphymysql.Error("alert", err)
	}
	out := make([]string, 0, len(instances))
	for _, instance := range instances {
		if instance != nil {
			out = append(out, *instance)
		}
	}
	return out, nil
}

func (r *Repository) PurgeHandledMessages(ctx context.Context, before time.Time) (_ int64, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.queries.PurgeHandledMessages(ctx, before)
}

func toIncident(row GetIncidentRow) *murphy.Incident {
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
