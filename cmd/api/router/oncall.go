package router

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) ListSchedules(ctx context.Context, request ListSchedulesRequestObject) (_ ListSchedulesResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	schedules, next, err := s.oncall.ListSchedules(ctx, text(request.Params.TeamId), page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Schedule, len(schedules))
	for i, schedule := range schedules {
		items[i] = toSchedule(schedule)
	}
	return ListSchedules200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) CreateSchedule(ctx context.Context, request CreateScheduleRequestObject) (_ CreateScheduleResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	schedule, err := s.oncall.CreateSchedule(ctx, fromScheduleInput(request.Body))
	if err != nil {
		return nil, err
	}
	return CreateSchedule201JSONResponse(toSchedule(schedule)), nil
}

func (s *Server) GetSchedule(ctx context.Context, request GetScheduleRequestObject) (_ GetScheduleResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	schedule, err := s.oncall.GetSchedule(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetSchedule200JSONResponse(toSchedule(schedule)), nil
}

func (s *Server) UpdateSchedule(ctx context.Context, request UpdateScheduleRequestObject) (_ UpdateScheduleResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	in := fromScheduleInput(request.Body)
	in.Id = request.Id
	schedule, err := s.oncall.UpdateSchedule(ctx, in)
	if err != nil {
		return nil, err
	}
	return UpdateSchedule200JSONResponse(toSchedule(schedule)), nil
}

func (s *Server) DeleteSchedule(ctx context.Context, request DeleteScheduleRequestObject) (_ DeleteScheduleResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.oncall.DeleteSchedule(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteSchedule204Response{}, nil
}

func (s *Server) ListOverrides(ctx context.Context, request ListOverridesRequestObject) (_ ListOverridesResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	endsAfter := time.Now()
	if request.Params.EndsAfter != nil {
		endsAfter = *request.Params.EndsAfter
	}
	overrides, err := s.oncall.ListOverrides(ctx, request.Id, endsAfter)
	if err != nil {
		return nil, err
	}
	items := make([]Override, len(overrides))
	for i, override := range overrides {
		items[i] = toOverride(override)
	}
	return ListOverrides200JSONResponse{Items: items}, nil
}

func (s *Server) CreateOverride(ctx context.Context, request CreateOverrideRequestObject) (_ CreateOverrideResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	override, err := s.oncall.CreateOverride(ctx, &murphy.Override{
		ScheduleId: request.Id,
		UserId:     request.Body.User.Id,
		StartsAt:   timestamppb.New(request.Body.StartsAt),
		EndsAt:     timestamppb.New(request.Body.EndsAt),
	})
	if err != nil {
		return nil, err
	}
	return CreateOverride201JSONResponse(toOverride(override)), nil
}

func (s *Server) DeleteOverride(ctx context.Context, request DeleteOverrideRequestObject) (_ DeleteOverrideResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.oncall.DeleteOverride(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteOverride204Response{}, nil
}

func (s *Server) GetOnCall(ctx context.Context, request GetOnCallRequestObject) (_ GetOnCallResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	at := time.Now().UTC()
	if request.Params.At != nil {
		at = *request.Params.At
	}
	user, err := s.oncall.OnCall(ctx, request.Id, at)
	if err != nil {
		return nil, err
	}
	out := GetOnCall200JSONResponse{At: at}
	if user != "" {
		reference := ref(user)
		out.User = &reference
	}
	return out, nil
}

func fromScheduleInput(in *ScheduleInput) *murphy.Schedule {
	schedule := &murphy.Schedule{Name: in.Name, TimeZone: text(in.TimeZone)}
	if in.Teams != nil {
		schedule.TeamIds = ids(*in.Teams)
	}
	for _, layer := range in.Layers {
		schedule.Layers = append(schedule.Layers, &murphy.Layer{
			Name:         text(layer.Name),
			Participants: ids(layer.Participants),
			TurnLength:   durationpb.New(time.Duration(layer.TurnLengthSeconds) * time.Second),
			StartsAt:     timestamppb.New(layer.StartsAt),
		})
	}
	return schedule
}

func toSchedule(schedule *murphy.Schedule) Schedule {
	out := Schedule{
		Id:        schedule.GetId(),
		Name:      schedule.GetName(),
		TimeZone:  schedule.GetTimeZone(),
		Teams:     refs(schedule.GetTeamIds()),
		Layers:    []Layer{},
		CreatedAt: schedule.GetCreatedAt().AsTime(),
		UpdatedAt: schedule.GetUpdatedAt().AsTime(),
	}
	for _, layer := range schedule.GetLayers() {
		l := Layer{
			Participants:      refs(layer.GetParticipants()),
			TurnLengthSeconds: int64(layer.GetTurnLength().AsDuration() / time.Second),
			StartsAt:          layer.GetStartsAt().AsTime(),
		}
		if name := layer.GetName(); name != "" {
			l.Name = &name
		}
		out.Layers = append(out.Layers, l)
	}
	return out
}

func toOverride(override *murphy.Override) Override {
	return Override{
		Id:        override.GetId(),
		Schedule:  ref(override.GetScheduleId()),
		User:      ref(override.GetUserId()),
		StartsAt:  override.GetStartsAt().AsTime(),
		EndsAt:    override.GetEndsAt().AsTime(),
		CreatedAt: override.GetCreatedAt().AsTime(),
	}
}
