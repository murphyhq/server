package mysql

import (
	"context"
	"embed"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
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
		Service:    "oncall",
		Migrations: migrations,
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{tx: tx, queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

type Tx struct {
	tx      murphymysql.DBTX
	queries *Queries
}

func (t *Tx) CreateScheduleRow(ctx context.Context, schedule *murphy.Schedule) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("schedule", t.queries.CreateSchedule(ctx, CreateScheduleParams{
		ID: schedule.GetId(), Name: schedule.GetName(), TimeZone: schedule.GetTimeZone(),
		CreatedAt: schedule.GetCreatedAt().AsTime(), UpdatedAt: schedule.GetUpdatedAt().AsTime(),
	}))
}

func (t *Tx) UpdateScheduleRow(ctx context.Context, schedule *murphy.Schedule) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.UpdateSchedule(ctx, UpdateScheduleParams{
		ID: schedule.GetId(), Name: schedule.GetName(), TimeZone: schedule.GetTimeZone(), UpdatedAt: schedule.GetUpdatedAt().AsTime(),
	})
	return murphymysql.Changed("schedule", rows, err)
}

func (t *Tx) DeleteScheduleTeams(ctx context.Context, scheduleID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("schedule team", t.queries.DeleteScheduleTeams(ctx, scheduleID))
}

func (t *Tx) DeleteLayers(ctx context.Context, scheduleID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("layer", t.queries.DeleteLayers(ctx, scheduleID))
}

func (t *Tx) CreateScheduleTeams(ctx context.Context, scheduleID string, teamIDs []string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateScheduleTeams", Table: "schedule_teams", Columns: []string{"id", "schedule_id", "team_id", "created_at"}}
	for _, team := range teamIDs {
		rows.Values = append(rows.Values, []any{murphy.NewID(), scheduleID, team, at})
	}
	return murphymysql.Error("schedule team", murphymysql.Insert(ctx, t.tx, rows))
}

func (t *Tx) CreateLayers(ctx context.Context, scheduleID string, layers []*murphy.Layer, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{
		Name:    "CreateLayers",
		Table:   "layers",
		Columns: []string{"id", "schedule_id", "position", "name", "turn_length_seconds", "starts_at", "created_at"},
	}
	for i, layer := range layers {
		rows.Values = append(rows.Values, []any{
			layer.GetId(), scheduleID, i, layer.GetName(), int64(layer.GetTurnLength().AsDuration() / time.Second), layer.GetStartsAt().AsTime(), at,
		})
	}
	return murphymysql.Error("layer", murphymysql.Insert(ctx, t.tx, rows))
}

func (t *Tx) CreateLayerParticipants(ctx context.Context, layers []*murphy.Layer, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateLayerParticipants", Table: "layer_participants", Columns: []string{"id", "layer_id", "position", "user_id", "created_at"}}
	for _, layer := range layers {
		for i, user := range layer.GetParticipants() {
			rows.Values = append(rows.Values, []any{murphy.NewID(), layer.GetId(), i, user, at})
		}
	}
	return murphymysql.Error("layer participant", murphymysql.Insert(ctx, t.tx, rows))
}

func (r *Repository) GetScheduleRow(ctx context.Context, id string) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetSchedule(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("schedule", err)
	}
	return toSchedule(row), nil
}

func (r *Repository) ListScheduleRows(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListSchedules(ctx, ListSchedulesParams{Before: before, TeamID: database.Text(teamID), Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("schedule", err)
	}
	schedules := make([]*murphy.Schedule, len(rows))
	for i, row := range rows {
		schedules[i] = toSchedule(row)
	}
	return schedules, nil
}

func (r *Repository) ListScheduleTeams(ctx context.Context, scheduleIDs []string) (_ map[string][]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListScheduleTeams(ctx, scheduleIDs)
	if err != nil {
		return nil, murphymysql.Error("schedule team", err)
	}
	teams := map[string][]string{}
	for _, row := range rows {
		teams[row.ScheduleID] = append(teams[row.ScheduleID], row.TeamID)
	}
	return teams, nil
}

func (r *Repository) ListLayers(ctx context.Context, scheduleIDs []string) (_ map[string][]*murphy.Layer, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListLayers(ctx, scheduleIDs)
	if err != nil {
		return nil, murphymysql.Error("layer", err)
	}
	layers := map[string][]*murphy.Layer{}
	for _, row := range rows {
		layers[row.ScheduleID] = append(layers[row.ScheduleID], &murphy.Layer{
			Id: row.ID, Name: row.Name,
			TurnLength: durationpb.New(time.Duration(row.TurnLengthSeconds) * time.Second), StartsAt: timestamppb.New(row.StartsAt),
		})
	}
	return layers, nil
}

func (r *Repository) ListLayerParticipants(ctx context.Context, scheduleIDs []string) (_ map[string][]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListLayerParticipants(ctx, scheduleIDs)
	if err != nil {
		return nil, murphymysql.Error("layer participant", err)
	}
	participants := map[string][]string{}
	for _, row := range rows {
		participants[row.LayerID] = append(participants[row.LayerID], row.UserID)
	}
	return participants, nil
}

func toSchedule(row Schedule) *murphy.Schedule {
	return &murphy.Schedule{
		Id: row.ID, Name: row.Name, TimeZone: row.TimeZone,
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}
}

func (r *Repository) DeleteSchedule(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteSchedule(ctx, id)
	return murphymysql.Changed("schedule", rows, err)
}

func (r *Repository) CreateOverride(ctx context.Context, override *murphy.Override) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("override", r.queries.CreateOverride(ctx, CreateOverrideParams{
		ID: override.GetId(), ScheduleID: override.GetScheduleId(), UserID: override.GetUserId(),
		StartsAt: override.GetStartsAt().AsTime(), EndsAt: override.GetEndsAt().AsTime(),
		CreatedAt: override.GetCreatedAt().AsTime(), UpdatedAt: override.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) ListOverrides(ctx context.Context, scheduleID string, endsAfter time.Time) (_ []*murphy.Override, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListOverrides(ctx, ListOverridesParams{ScheduleID: scheduleID, EndsAfter: endsAfter})
	if err != nil {
		return nil, murphymysql.Error("override", err)
	}
	overrides := make([]*murphy.Override, len(rows))
	for i, row := range rows {
		overrides[i] = &murphy.Override{
			Id: row.ID, ScheduleId: row.ScheduleID, UserId: row.UserID,
			StartsAt: timestamppb.New(row.StartsAt), EndsAt: timestamppb.New(row.EndsAt),
			CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
		}
	}
	return overrides, nil
}

func (r *Repository) GetOverride(ctx context.Context, id string) (_ *murphy.Override, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetOverride(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("override", err)
	}
	return &murphy.Override{
		Id: row.ID, ScheduleId: row.ScheduleID, UserId: row.UserID,
		StartsAt: timestamppb.New(row.StartsAt), EndsAt: timestamppb.New(row.EndsAt),
		CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
	}, nil
}

func (r *Repository) DeleteOverride(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteOverride(ctx, id)
	return murphymysql.Changed("override", rows, err)
}

func (r *Repository) ExistingSchedules(ctx context.Context, ids []string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	existing, err := r.queries.ExistingSchedules(ctx, ids)
	if err != nil {
		return nil, murphymysql.Error("schedule", err)
	}
	return existing, nil
}

func (r *Repository) GetScheduleRows(ctx context.Context, ids []string) (_ []*murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.GetSchedules(ctx, ids)
	if err != nil {
		return nil, murphymysql.Error("schedule", err)
	}
	schedules := make([]*murphy.Schedule, len(rows))
	for i, row := range rows {
		schedules[i] = toSchedule(row)
	}
	return schedules, nil
}

func (r *Repository) ListOverridesAt(ctx context.Context, scheduleIDs []string, at time.Time) (_ map[string][]*murphy.Override, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListOverridesAt(ctx, ListOverridesAtParams{ScheduleIds: scheduleIDs, At: at, After: at})
	if err != nil {
		return nil, murphymysql.Error("override", err)
	}
	overrides := map[string][]*murphy.Override{}
	for _, row := range rows {
		overrides[row.ScheduleID] = append(overrides[row.ScheduleID], &murphy.Override{
			Id: row.ID, ScheduleId: row.ScheduleID, UserId: row.UserID,
			StartsAt: timestamppb.New(row.StartsAt), EndsAt: timestamppb.New(row.EndsAt),
			CreatedAt: timestamppb.New(row.CreatedAt), UpdatedAt: timestamppb.New(row.UpdatedAt),
		})
	}
	return overrides, nil
}
