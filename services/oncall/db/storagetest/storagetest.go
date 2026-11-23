package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/oncall"
)

const maxID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

var start = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

func Run(t *testing.T, open func(t *testing.T) oncall.Repository) {
	t.Run("stores and reads back a schedule with its layers in order", func(t *testing.T) {
		repo := open(t)
		ana, bo, cy := murphy.NewID(), murphy.NewID(), murphy.NewID()
		team := murphy.NewID()
		schedule := newSchedule("primary", []string{team},
			layer("weekdays", 24*time.Hour, cy, ana, bo),
			layer("weekends", 7*24*time.Hour, bo),
		)

		require.NoError(t, repo.CreateSchedule(t.Context(), schedule))
		got, err := repo.GetSchedule(t.Context(), schedule.GetId())

		require.NoError(t, err)
		assert.Equal(t, "primary", got.GetName())
		assert.Equal(t, "Europe/Malta", got.GetTimeZone())
		assert.Equal(t, []string{team}, got.GetTeamIds())
		require.Len(t, got.GetLayers(), 2)
		assert.Equal(t, "weekdays", got.GetLayers()[0].GetName())
		assert.Equal(t, []string{cy, ana, bo}, got.GetLayers()[0].GetParticipants())
		assert.Equal(t, 24*time.Hour, got.GetLayers()[0].GetTurnLength().AsDuration())
		assert.Equal(t, start, got.GetLayers()[0].GetStartsAt().AsTime())
		assert.Equal(t, "weekends", got.GetLayers()[1].GetName())
		assert.Equal(t, []string{bo}, got.GetLayers()[1].GetParticipants())
	})

	t.Run("updating a schedule replaces its teams and layers", func(t *testing.T) {
		repo := open(t)
		ana, bo := murphy.NewID(), murphy.NewID()
		schedule := newSchedule("primary", []string{murphy.NewID(), murphy.NewID()}, layer("old", time.Hour, ana, bo))
		require.NoError(t, repo.CreateSchedule(t.Context(), schedule))

		schedule.TeamIds = nil
		schedule.Layers = []*murphy.Layer{layer("new", 2*time.Hour, bo)}
		require.NoError(t, repo.UpdateSchedule(t.Context(), schedule))

		got, err := repo.GetSchedule(t.Context(), schedule.GetId())
		require.NoError(t, err)
		assert.Empty(t, got.GetTeamIds())
		require.Len(t, got.GetLayers(), 1)
		assert.Equal(t, "new", got.GetLayers()[0].GetName())
		assert.Equal(t, []string{bo}, got.GetLayers()[0].GetParticipants())
	})

	t.Run("lists schedules newest first, optionally of one team", func(t *testing.T) {
		repo := open(t)
		platform, payments := murphy.NewID(), murphy.NewID()
		shared := newSchedule("shared", []string{platform, payments}, layer("l", time.Hour, murphy.NewID()))
		own := newSchedule("own", []string{payments}, layer("l", time.Hour, murphy.NewID()))
		none := newSchedule("none", nil, layer("l", time.Hour, murphy.NewID()))
		for _, schedule := range []*murphy.Schedule{shared, own, none} {
			require.NoError(t, repo.CreateSchedule(t.Context(), schedule))
		}

		all, err := repo.ListSchedules(t.Context(), "", maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{none.GetId(), own.GetId(), shared.GetId()}, scheduleIDs(all))

		ofPayments, err := repo.ListSchedules(t.Context(), payments, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{own.GetId(), shared.GetId()}, scheduleIDs(ofPayments))
		assert.ElementsMatch(t, []string{platform, payments}, ofPayments[1].GetTeamIds())

		page, err := repo.ListSchedules(t.Context(), payments, own.GetId(), 10)
		require.NoError(t, err)
		assert.Equal(t, []string{shared.GetId()}, scheduleIDs(page))
	})

	t.Run("lists overrides that have not ended, by start", func(t *testing.T) {
		repo := open(t)
		schedule := newSchedule("primary", nil, layer("l", time.Hour, murphy.NewID()))
		require.NoError(t, repo.CreateSchedule(t.Context(), schedule))
		past := newOverride(schedule, start.Add(-2*time.Hour), start.Add(-time.Hour))
		later := newOverride(schedule, start.Add(2*time.Hour), start.Add(3*time.Hour))
		current := newOverride(schedule, start.Add(-time.Hour), start.Add(time.Hour))
		for _, override := range []*murphy.Override{past, later, current} {
			require.NoError(t, repo.CreateOverride(t.Context(), override))
		}

		got, err := repo.ListOverrides(t.Context(), schedule.GetId(), start)

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, current.GetId(), got[0].GetId())
		assert.Equal(t, later.GetId(), got[1].GetId())
	})

	t.Run("deleting a schedule deletes its overrides", func(t *testing.T) {
		repo := open(t)
		schedule := newSchedule("primary", nil, layer("l", time.Hour, murphy.NewID()))
		require.NoError(t, repo.CreateSchedule(t.Context(), schedule))
		override := newOverride(schedule, start, start.Add(time.Hour))
		require.NoError(t, repo.CreateOverride(t.Context(), override))

		require.NoError(t, repo.DeleteSchedule(t.Context(), schedule.GetId()))

		assert.ErrorIs(t, repo.DeleteOverride(t.Context(), override.GetId()), murphy.ErrNotFound)
	})

	t.Run("finds which of many schedules exist in one call", func(t *testing.T) {
		repo := open(t)
		first := newSchedule("first", nil, layer("l", time.Hour, murphy.NewID()))
		second := newSchedule("second", nil, layer("l", time.Hour, murphy.NewID()))
		require.NoError(t, repo.CreateSchedule(t.Context(), first))
		require.NoError(t, repo.CreateSchedule(t.Context(), second))

		got, err := repo.ExistingSchedules(t.Context(), []string{second.GetId(), murphy.NewID(), first.GetId()})

		require.NoError(t, err)
		assert.ElementsMatch(t, []string{first.GetId(), second.GetId()}, got)
	})

	t.Run("reports missing schedules and overrides", func(t *testing.T) {
		repo := open(t)
		_, err := repo.GetSchedule(t.Context(), murphy.NewID())
		assert.ErrorIs(t, err, murphy.ErrNotFound)
		ghost := newSchedule("ghost", nil, layer("l", time.Hour, murphy.NewID()))
		assert.ErrorIs(t, repo.UpdateSchedule(t.Context(), ghost), murphy.ErrNotFound)
		assert.ErrorIs(t, repo.DeleteSchedule(t.Context(), murphy.NewID()), murphy.ErrNotFound)
		assert.ErrorIs(t, repo.DeleteOverride(t.Context(), murphy.NewID()), murphy.ErrNotFound)
	})

	t.Run("refuses a second schedule with the same name", func(t *testing.T) {
		repo := open(t)
		require.NoError(t, repo.CreateSchedule(t.Context(), newSchedule("primary", nil, layer("l", time.Hour, murphy.NewID()))))

		err := repo.CreateSchedule(t.Context(), newSchedule("primary", nil, layer("l", time.Hour, murphy.NewID())))

		assert.ErrorIs(t, err, murphy.ErrConflict)
	})
}

func newSchedule(name string, teams []string, layers ...*murphy.Layer) *murphy.Schedule {
	now := murphy.Now()
	return &murphy.Schedule{
		Id: murphy.NewID(), Name: name, TimeZone: "Europe/Malta", TeamIds: teams, Layers: layers,
		CreatedAt: now, UpdatedAt: now,
	}
}

func layer(name string, turn time.Duration, participants ...string) *murphy.Layer {
	return &murphy.Layer{Name: name, Participants: participants, TurnLength: durationpb.New(turn), StartsAt: timestamppb.New(start)}
}

func newOverride(schedule *murphy.Schedule, from, to time.Time) *murphy.Override {
	now := murphy.Now()
	return &murphy.Override{
		Id: murphy.NewID(), ScheduleId: schedule.GetId(), UserId: murphy.NewID(),
		StartsAt: timestamppb.New(from), EndsAt: timestamppb.New(to), CreatedAt: now, UpdatedAt: now,
	}
}

func scheduleIDs(schedules []*murphy.Schedule) []string {
	ids := make([]string, len(schedules))
	for i, schedule := range schedules {
		ids[i] = schedule.GetId()
	}
	return ids
}
