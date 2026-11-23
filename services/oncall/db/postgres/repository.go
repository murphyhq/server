package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
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
		return fmt.Errorf("load oncall migrations: %w", err)
	}
	return murphypostgres.Migrate(ctx, r.pool, murphypostgres.Migrations{Service: "oncall", Files: files})
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) Transaction(ctx context.Context, work func(tx *Tx) error) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := work(&Tx{queries: r.queries.WithTx(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return murphypostgres.Error("schedule", err)
	}
	return nil
}

type Tx struct {
	queries *Queries
}

func (t *Tx) CreateScheduleRow(ctx context.Context, schedule *murphy.Schedule) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("schedule", t.queries.CreateSchedule(ctx, CreateScheduleParams{
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
	return murphypostgres.Changed("schedule", rows, err)
}

func (t *Tx) DeleteScheduleTeams(ctx context.Context, scheduleID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("schedule team", t.queries.DeleteScheduleTeams(ctx, scheduleID))
}

func (t *Tx) DeleteLayers(ctx context.Context, scheduleID string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("layer", t.queries.DeleteLayers(ctx, scheduleID))
}

func (t *Tx) CreateScheduleTeams(ctx context.Context, scheduleID string, teamIDs []string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(teamIDs) == 0 {
		return nil
	}
	ids := make([]string, len(teamIDs))
	for i := range ids {
		ids[i] = murphy.NewID()
	}
	return murphypostgres.Error("schedule team", t.queries.CreateScheduleTeams(ctx, CreateScheduleTeamsParams{
		Ids: ids, ScheduleID: scheduleID, TeamIds: teamIDs, CreatedAt: at,
	}))
}

func (t *Tx) CreateLayers(ctx context.Context, scheduleID string, layers []*murphy.Layer, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(layers) == 0 {
		return nil
	}
	params := CreateLayersParams{ScheduleID: scheduleID, CreatedAt: at}
	for i, layer := range layers {
		params.Ids = append(params.Ids, layer.GetId())
		params.Positions = append(params.Positions, int32(i))
		params.Names = append(params.Names, layer.GetName())
		params.TurnLengthSeconds = append(params.TurnLengthSeconds, int64(layer.GetTurnLength().AsDuration()/time.Second))
		params.StartsAt = append(params.StartsAt, layer.GetStartsAt().AsTime())
	}
	return murphypostgres.Error("layer", t.queries.CreateLayers(ctx, params))
}

func (t *Tx) CreateLayerParticipants(ctx context.Context, layers []*murphy.Layer, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	params := CreateLayerParticipantsParams{CreatedAt: at}
	for _, layer := range layers {
		for i, user := range layer.GetParticipants() {
			params.Ids = append(params.Ids, murphy.NewID())
			params.LayerIds = append(params.LayerIds, layer.GetId())
			params.Positions = append(params.Positions, int32(i))
			params.UserIds = append(params.UserIds, user)
		}
	}
	if len(params.Ids) == 0 {
		return nil
	}
	return murphypostgres.Error("layer participant", t.queries.CreateLayerParticipants(ctx, params))
}

func (r *Repository) GetScheduleRow(ctx context.Context, id string) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetSchedule(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("schedule", err)
	}
	return toSchedule(row), nil
}

func (r *Repository) ListScheduleRows(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListSchedules(ctx, ListSchedulesParams{Before: before, TeamID: database.Text(teamID), MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("schedule", err)
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
		return nil, murphypostgres.Error("schedule team", err)
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
		return nil, murphypostgres.Error("layer", err)
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
		return nil, murphypostgres.Error("layer participant", err)
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
	return murphypostgres.Changed("schedule", rows, err)
}

func (r *Repository) CreateOverride(ctx context.Context, override *murphy.Override) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("override", r.queries.CreateOverride(ctx, CreateOverrideParams{
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
		return nil, murphypostgres.Error("override", err)
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

func (r *Repository) DeleteOverride(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteOverride(ctx, id)
	return murphypostgres.Changed("override", rows, err)
}

func (r *Repository) ExistingSchedules(ctx context.Context, ids []string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	existing, err := r.queries.ExistingSchedules(ctx, ids)
	if err != nil {
		return nil, murphypostgres.Error("schedule", err)
	}
	return existing, nil
}
