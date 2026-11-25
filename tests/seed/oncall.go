package main

import (
	"context"
	"net/http"
	"time"

	"github.com/murphyhq/server/client"
)

var rotationStart = time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)

func seedSchedules(ctx context.Context, api *client.ClientWithResponses, teams, users map[string]string) (map[string]string, error) {
	const day, week = int64(24 * 60 * 60), int64(7 * 24 * 60 * 60)
	malta := "Europe/Malta"
	schedules := []client.ScheduleInput{
		{
			Name: "platform primary", TimeZone: &malta, Teams: &[]client.Reference{{Id: teams["platform"]}},
			Layers: []client.Layer{{Name: new("weekly"), TurnLengthSeconds: week, StartsAt: rotationStart, Participants: []client.Reference{{Id: users["ana@murphy.test"]}, {Id: users["bo@murphy.test"]}}}},
		},
		{
			Name: "platform secondary", TimeZone: &malta, Teams: &[]client.Reference{{Id: teams["platform"]}},
			Layers: []client.Layer{{Name: new("weekly"), TurnLengthSeconds: week, StartsAt: rotationStart, Participants: []client.Reference{{Id: users["bo@murphy.test"]}, {Id: users["ana@murphy.test"]}}}},
		},
		{
			Name: "payments primary", TimeZone: &malta, Teams: &[]client.Reference{{Id: teams["payments"]}},
			Layers: []client.Layer{{Name: new("daily"), TurnLengthSeconds: day, StartsAt: rotationStart, Participants: []client.Reference{{Id: users["cy@murphy.test"]}, {Id: users["dee@murphy.test"]}}}},
		},
		{
			Name: "incident commanders", TimeZone: &malta, Teams: &[]client.Reference{{Id: teams["platform"]}, {Id: teams["payments"]}},
			Layers: []client.Layer{{Name: new("weekly"), TurnLengthSeconds: week, StartsAt: rotationStart, Participants: []client.Reference{{Id: users["ana@murphy.test"]}, {Id: users["dee@murphy.test"]}}}},
		},
		{
			Name: "support", TimeZone: &malta, Teams: &[]client.Reference{{Id: teams["platform"]}, {Id: teams["payments"]}},
			Layers: []client.Layer{{Name: new("daily"), TurnLengthSeconds: day, StartsAt: rotationStart, Participants: []client.Reference{
				{Id: users["ana@murphy.test"]}, {Id: users["bo@murphy.test"]}, {Id: users["cy@murphy.test"]}, {Id: users["dee@murphy.test"]},
			}}},
		},
	}

	existing, err := byName(func(cursor *string) ([]client.Schedule, string, error) {
		page, err := api.ListSchedulesWithResponse(ctx, &client.ListSchedulesParams{Cursor: cursor})
		if err := expect(page, err, http.StatusOK, "list schedules"); err != nil {
			return nil, "", err
		}
		return page.JSON200.Items, page.JSON200.NextCursor, nil
	}, func(schedule client.Schedule) (string, string) { return schedule.Name, schedule.Id })
	if err != nil {
		return nil, err
	}
	for _, schedule := range schedules {
		if _, ok := existing[schedule.Name]; ok {
			continue
		}
		created, err := api.CreateScheduleWithResponse(ctx, nil, schedule)
		if err := expect(created, err, http.StatusCreated, "create schedule "+schedule.Name); err != nil {
			return nil, err
		}
		existing[schedule.Name] = created.JSON201.Id
	}
	return existing, nil
}
