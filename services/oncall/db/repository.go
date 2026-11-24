package db

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/oncall"
)

type Tx interface {
	CreateScheduleRow(ctx context.Context, schedule *murphy.Schedule) error
	UpdateScheduleRow(ctx context.Context, schedule *murphy.Schedule) error
	DeleteScheduleTeams(ctx context.Context, scheduleID string) error
	CreateScheduleTeams(ctx context.Context, scheduleID string, teamIDs []string, at time.Time) error
	DeleteLayers(ctx context.Context, scheduleID string) error
	CreateLayers(ctx context.Context, scheduleID string, layers []*murphy.Layer, at time.Time) error
	CreateLayerParticipants(ctx context.Context, layers []*murphy.Layer, at time.Time) error
}

type Engine[T Tx] interface {
	GetScheduleRow(ctx context.Context, id string) (*murphy.Schedule, error)
	GetScheduleRows(ctx context.Context, ids []string) ([]*murphy.Schedule, error)
	ListOverridesAt(ctx context.Context, scheduleIDs []string, at time.Time) (map[string][]*murphy.Override, error)
	ListScheduleRows(ctx context.Context, teamID string, before string, limit int) ([]*murphy.Schedule, error)
	ListScheduleTeams(ctx context.Context, scheduleIDs []string) (map[string][]string, error)
	ListLayers(ctx context.Context, scheduleIDs []string) (map[string][]*murphy.Layer, error)
	ListLayerParticipants(ctx context.Context, scheduleIDs []string) (map[string][]string, error)
	DeleteSchedule(ctx context.Context, id string) error
	CreateOverride(ctx context.Context, override *murphy.Override) error
	ListOverrides(ctx context.Context, scheduleID string, endsAfter time.Time) ([]*murphy.Override, error)
	GetOverride(ctx context.Context, id string) (*murphy.Override, error)
	DeleteOverride(ctx context.Context, id string) error
	ScheduleNames(ctx context.Context, ids []string) (map[string]string, error)
	TeamsOfSchedule(ctx context.Context, id string) ([]string, error)
	SchedulesOfUsers(ctx context.Context, userIDs []string, at time.Time, before string, limit int) ([]string, error)
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var _ oncall.Repository = (*Repository[Tx])(nil)

func (r *Repository[T]) CreateSchedule(ctx context.Context, schedule *murphy.Schedule) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Transaction(ctx, func(tx T) error {
		if err := tx.CreateScheduleRow(ctx, schedule); err != nil {
			return err
		}
		return writeChildren(ctx, tx, schedule)
	})
}

func (r *Repository[T]) UpdateSchedule(ctx context.Context, schedule *murphy.Schedule) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.Transaction(ctx, func(tx T) error {
		if err := tx.UpdateScheduleRow(ctx, schedule); err != nil {
			return err
		}
		if err := tx.DeleteScheduleTeams(ctx, schedule.GetId()); err != nil {
			return err
		}
		if err := tx.DeleteLayers(ctx, schedule.GetId()); err != nil {
			return err
		}
		return writeChildren(ctx, tx, schedule)
	})
}

func writeChildren[T Tx](ctx context.Context, tx T, schedule *murphy.Schedule) error {
	at := schedule.GetUpdatedAt().AsTime()
	for _, layer := range schedule.GetLayers() {
		layer.Id = murphy.NewID()
	}
	if err := tx.CreateScheduleTeams(ctx, schedule.GetId(), schedule.GetTeamIds(), at); err != nil {
		return err
	}
	if err := tx.CreateLayers(ctx, schedule.GetId(), schedule.GetLayers(), at); err != nil {
		return err
	}
	return tx.CreateLayerParticipants(ctx, schedule.GetLayers(), at)
}

func (r *Repository[T]) GetSchedule(ctx context.Context, id string) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	schedule, err := r.GetScheduleRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, []*murphy.Schedule{schedule}); err != nil {
		return nil, err
	}
	return schedule, nil
}

func (r *Repository[T]) ListSchedules(ctx context.Context, teamID string, before string, limit int) (_ []*murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	schedules, err := r.ListScheduleRows(ctx, teamID, before, limit)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, schedules); err != nil {
		return nil, err
	}
	return schedules, nil
}

func (r *Repository[T]) complete(ctx context.Context, schedules []*murphy.Schedule) error {
	if len(schedules) == 0 {
		return nil
	}
	ids := make([]string, len(schedules))
	for i, schedule := range schedules {
		ids[i] = schedule.GetId()
	}
	teams, err := r.ListScheduleTeams(ctx, ids)
	if err != nil {
		return err
	}
	layers, err := r.ListLayers(ctx, ids)
	if err != nil {
		return err
	}
	participants, err := r.ListLayerParticipants(ctx, ids)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		schedule.TeamIds = append([]string{}, teams[schedule.GetId()]...)
		schedule.Layers = layers[schedule.GetId()]
		for _, layer := range schedule.GetLayers() {
			layer.Participants = participants[layer.GetId()]
		}
	}
	return nil
}

func (r *Repository[T]) GetSchedules(ctx context.Context, ids []string) (_ []*murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return nil, nil
	}
	schedules, err := r.GetScheduleRows(ctx, ids)
	if err != nil {
		return nil, err
	}
	if err := r.complete(ctx, schedules); err != nil {
		return nil, err
	}
	return schedules, nil
}

func (r *Repository[T]) GetScheduleRecord(ctx context.Context, id string) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return r.GetScheduleRow(ctx, id)
}
