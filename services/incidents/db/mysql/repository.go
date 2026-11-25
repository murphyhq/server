package mysql

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/incidents"
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
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{tx: tx, queries: New(tx)} },
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

func (r *Repository) ListIncidents(ctx context.Context, filter incidents.Filter, before string, limit int) (_ []*murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListIncidents(ctx, ListIncidentsParams{
		Before:              before,
		AnyStatus:           len(filter.Statuses) == 0,
		Statuses:            texts(filter.Statuses),
		AnyService:          len(filter.ServiceIDs) == 0,
		ServiceIds:          filter.ServiceIDs,
		AnyPolicy:           len(filter.PolicyIDs) == 0,
		EscalationPolicyIds: filter.PolicyIDs,
		AnyUrgency:          len(filter.Urgencies) == 0,
		Urgencies:           texts(filter.Urgencies),
		AnyonePaged:         len(filter.PagedUserIDs) == 0,
		PagedUserIds:        filter.PagedUserIDs,
		CreatedAfter:        filter.CreatedAfter,
		CreatedBefore:       filter.CreatedBefore,
		Limit:               int32(limit),
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
		entry := &murphy.TimelineEntry{}
		if err := proto.Unmarshal(row.Details, entry); err != nil {
			return nil, fmt.Errorf("decode timeline entry %s: %w", row.ID, err)
		}
		entries[i] = entry
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

		EscalationPolicyId:      row.EscalationPolicyID,
		EscalationPolicyVersion: row.EscalationPolicyVersion,
		EscalationLevel:         row.EscalationLevel,
		EscalationRound:         row.EscalationRound,
		EscalationExhausted:     row.EscalationExhausted,
		AcknowledgedAt:          database.Timestamp(row.AcknowledgedAt),
	}
}

func (r *Repository) DeleteMessages(ctx context.Context, ids []string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return nil
	}
	return murphymysql.Error("scheduled message", r.queries.DeleteMessages(ctx, ids))
}

func texts[T interface{ Text() string }](values []T) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = value.Text()
	}
	return out
}

func (r *Repository) PolicyOfIncident(ctx context.Context, id, userID string) (_ string, paged bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.PolicyOfIncident(ctx, PolicyOfIncidentParams{ID: id, UserID: userID})
	if err != nil {
		return "", false, murphymysql.Error("incident", err)
	}
	return row.EscalationPolicyID, row.Paged, nil
}
