package oncall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Repository interface {
	CreateSchedule(ctx context.Context, schedule *murphy.Schedule) error
	GetSchedule(ctx context.Context, id string) (*murphy.Schedule, error)
	GetScheduleRecord(ctx context.Context, id string) (*murphy.Schedule, error)
	TeamsOfSchedule(ctx context.Context, id string) ([]string, error)
	SchedulesOfUsers(ctx context.Context, userIDs []string, at time.Time, before string, limit int) ([]string, error)
	ListSchedules(ctx context.Context, teamID string, before string, limit int) ([]*murphy.Schedule, error)
	UpdateSchedule(ctx context.Context, schedule *murphy.Schedule) error
	DeleteSchedule(ctx context.Context, id string) error
	CreateOverride(ctx context.Context, override *murphy.Override) error
	ListOverrides(ctx context.Context, scheduleID string, endsAfter time.Time) ([]*murphy.Override, error)
	GetOverride(ctx context.Context, id string) (*murphy.Override, error)
	DeleteOverride(ctx context.Context, id string) error
	ScheduleNames(ctx context.Context, ids []string) (map[string]string, error)
	GetSchedules(ctx context.Context, ids []string) ([]*murphy.Schedule, error)
	ListOverridesAt(ctx context.Context, scheduleIDs []string, at time.Time) (map[string][]*murphy.Override, error)
}

type People interface {
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	TeamNames(ctx context.Context, ids []string) (map[string]string, error)
}

type Service struct {
	repo   Repository
	people People
}

func New(repo Repository, people People) *Service {
	return &Service{repo: repo, people: people}
}

func (s *Service) CreateSchedule(ctx context.Context, in *murphy.Schedule) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := murphy.Now()
	schedule := &murphy.Schedule{
		Id:        murphy.NewID(),
		Name:      in.GetName(),
		TimeZone:  murphy.Or(in.GetTimeZone(), "UTC"),
		Layers:    in.GetLayers(),
		TeamIds:   in.GetTeamIds(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", schedule.GetId()))
	if err := s.check(ctx, schedule); err != nil {
		return nil, err
	}
	if err := s.repo.CreateSchedule(ctx, schedule); err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return schedule, nil
}

func (s *Service) GetSchedule(ctx context.Context, id string) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("schedule", id); err != nil {
		return nil, err
	}
	schedule, err := s.repo.GetSchedule(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return schedule, nil
}

func (s *Service) ListSchedules(ctx context.Context, teamID string, page pagination.Page) (_ []*murphy.Schedule, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	if teamID != "" {
		if err := murphy.LookupID("team", teamID); err != nil {
			return nil, "", err
		}
	}
	schedules, err := s.repo.ListSchedules(ctx, teamID, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list schedules: %w", err)
	}
	items, next := pagination.Trim(schedules, window)
	return items, next, nil
}

func (s *Service) UpdateSchedule(ctx context.Context, in *murphy.Schedule) (_ *murphy.Schedule, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", in.GetId()))
	defer span.End(&err)

	if err := murphy.LookupID("schedule", in.GetId()); err != nil {
		return nil, err
	}
	current, err := s.repo.GetScheduleRecord(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	schedule := &murphy.Schedule{
		Id:        current.GetId(),
		Name:      in.GetName(),
		TimeZone:  murphy.Or(in.GetTimeZone(), current.GetTimeZone()),
		Layers:    in.GetLayers(),
		TeamIds:   in.GetTeamIds(),
		CreatedAt: current.GetCreatedAt(),
		UpdatedAt: murphy.Now(),
	}
	if err := s.check(ctx, schedule); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateSchedule(ctx, schedule); err != nil {
		return nil, fmt.Errorf("update schedule: %w", err)
	}
	return schedule, nil
}

func (s *Service) DeleteSchedule(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("schedule", id); err != nil {
		return err
	}
	if err := s.repo.DeleteSchedule(ctx, id); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

func (s *Service) CreateOverride(ctx context.Context, in *murphy.Override) (_ *murphy.Override, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", in.GetScheduleId()))
	defer span.End(&err)

	now := murphy.Now()
	override := &murphy.Override{
		Id:         murphy.NewID(),
		ScheduleId: in.GetScheduleId(),
		UserId:     in.GetUserId(),
		StartsAt:   in.GetStartsAt(),
		EndsAt:     in.GetEndsAt(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := override.Validate(); err != nil {
		return nil, err
	}
	schedules, err := s.ScheduleNames(ctx, []string{override.GetScheduleId()})
	if err != nil {
		return nil, err
	}
	if _, ok := schedules[override.GetScheduleId()]; !ok {
		return nil, &murphy.NotFoundError{Entity: "schedule"}
	}
	users, err := s.people.UserNames(ctx, []string{override.GetUserId()})
	if err != nil {
		return nil, fmt.Errorf("check user: %w", err)
	}
	if _, ok := users[override.GetUserId()]; !ok {
		return nil, &murphy.FieldError{Field: "user_id", Reason: "must be an existing user"}
	}
	if err := s.repo.CreateOverride(ctx, override); err != nil {
		return nil, fmt.Errorf("create override: %w", err)
	}
	return override, nil
}

func (s *Service) ListOverrides(ctx context.Context, scheduleID string, endsAfter time.Time) (scheduleName string, _ []*murphy.Override, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", scheduleID))
	defer span.End(&err)

	if err := murphy.LookupID("schedule", scheduleID); err != nil {
		return "", nil, err
	}
	names, err := s.ScheduleNames(ctx, []string{scheduleID})
	if err != nil {
		return "", nil, fmt.Errorf("find schedule: %w", err)
	}
	name, ok := names[scheduleID]
	if !ok {
		return "", nil, &murphy.NotFoundError{Entity: "schedule"}
	}
	overrides, err := s.repo.ListOverrides(ctx, scheduleID, endsAfter)
	if err != nil {
		return "", nil, fmt.Errorf("list overrides: %w", err)
	}
	return name, overrides, nil
}

func (s *Service) GetOverride(ctx context.Context, id string) (_ *murphy.Override, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("override.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("override", id); err != nil {
		return nil, err
	}
	override, err := s.repo.GetOverride(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get override: %w", err)
	}
	return override, nil
}

func (s *Service) DeleteOverride(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("override.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("override", id); err != nil {
		return err
	}
	if err := s.repo.DeleteOverride(ctx, id); err != nil {
		return fmt.Errorf("delete override: %w", err)
	}
	return nil
}

func (s *Service) OnCall(ctx context.Context, scheduleID string, at time.Time) (_ string, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", scheduleID))
	defer span.End(&err)

	schedule, err := s.GetSchedule(ctx, scheduleID)
	if err != nil {
		return "", err
	}
	overrides, err := s.repo.ListOverrides(ctx, scheduleID, at)
	if err != nil {
		return "", fmt.Errorf("list overrides: %w", err)
	}
	return onCall(schedule, at, overrides), nil
}

func (s *Service) check(ctx context.Context, schedule *murphy.Schedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}
	teams, err := s.people.TeamNames(ctx, schedule.GetTeamIds())
	if err != nil {
		return fmt.Errorf("check teams: %w", err)
	}
	var participants []string
	for _, layer := range schedule.GetLayers() {
		participants = append(participants, layer.GetParticipants()...)
	}
	users, err := s.people.UserNames(ctx, participants)
	if err != nil {
		return fmt.Errorf("check participants: %w", err)
	}

	var errs []error
	for i, team := range schedule.GetTeamIds() {
		if _, ok := teams[team]; !ok {
			errs = append(errs, &murphy.FieldError{Field: fmt.Sprintf("team_ids[%d]", i), Reason: "must be an existing team"})
		}
	}
	for i, layer := range schedule.GetLayers() {
		for j, participant := range layer.GetParticipants() {
			if _, ok := users[participant]; !ok {
				errs = append(errs, &murphy.FieldError{Field: fmt.Sprintf("layers[%d].participants[%d]", i, j), Reason: "must be an existing user"})
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) ScheduleNames(ctx context.Context, ids []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return s.repo.ScheduleNames(ctx, ids)
}

func (s *Service) WhoIsOnCall(ctx context.Context, scheduleIDs []string, at time.Time) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	onCallBy := map[string]string{}
	if len(scheduleIDs) == 0 {
		return onCallBy, nil
	}
	schedules, err := s.repo.GetSchedules(ctx, scheduleIDs)
	if err != nil {
		return nil, fmt.Errorf("get schedules: %w", err)
	}
	overrides, err := s.repo.ListOverridesAt(ctx, scheduleIDs, at)
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	for _, schedule := range schedules {
		if user := onCall(schedule, at, overrides[schedule.GetId()]); user != "" {
			onCallBy[schedule.GetId()] = user
		}
	}
	return onCallBy, nil
}

func (s *Service) TeamsOfSchedule(ctx context.Context, id string) (_ []string, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("schedule.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("schedule", id); err != nil {
		return nil, err
	}
	return s.repo.TeamsOfSchedule(ctx, id)
}

type OnCallNow struct {
	Schedule *murphy.Schedule
	UserID   string
}

func (s *Service) ListOnCall(ctx context.Context, userIDs []string, at time.Time, page pagination.Page) (_ []OnCallNow, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	for _, id := range userIDs {
		if err := murphy.LookupID("user", id); err != nil {
			return nil, "", err
		}
	}
	schedules, err := s.candidates(ctx, userIDs, at, window)
	if err != nil {
		return nil, "", err
	}
	schedules, next := pagination.Trim(schedules, window)
	if len(schedules) == 0 {
		return nil, next, nil
	}
	ids := make([]string, len(schedules))
	for i, schedule := range schedules {
		ids[i] = schedule.GetId()
	}
	overrides, err := s.repo.ListOverridesAt(ctx, ids, at)
	if err != nil {
		return nil, "", fmt.Errorf("list overrides: %w", err)
	}
	var found []OnCallNow
	for _, schedule := range schedules {
		user := onCall(schedule, at, overrides[schedule.GetId()])
		if len(userIDs) == 0 || slices.Contains(userIDs, user) {
			found = append(found, OnCallNow{Schedule: schedule, UserID: user})
		}
	}
	return found, next, nil
}

func (s *Service) candidates(ctx context.Context, userIDs []string, at time.Time, window pagination.Window) ([]*murphy.Schedule, error) {
	if len(userIDs) == 0 {
		schedules, err := s.repo.ListSchedules(ctx, "", window.Before, window.Rows())
		if err != nil {
			return nil, fmt.Errorf("list schedules: %w", err)
		}
		return schedules, nil
	}
	ids, err := s.repo.SchedulesOfUsers(ctx, userIDs, at, window.Before, window.Rows())
	if err != nil {
		return nil, fmt.Errorf("find the people's schedules: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	schedules, err := s.repo.GetSchedules(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get schedules: %w", err)
	}
	slices.SortFunc(schedules, func(a, b *murphy.Schedule) int { return strings.Compare(b.GetId(), a.GetId()) })
	return schedules, nil
}
