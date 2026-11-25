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
	rotation := func(name, zone string, turn int64, starts time.Time, teamNames, handles []string) client.ScheduleInput {
		owners := make([]client.Reference, len(teamNames))
		for i, team := range teamNames {
			owners[i] = client.Reference{Id: teams[team]}
		}
		participants := make([]client.Reference, len(handles))
		for i, handle := range handles {
			participants[i] = client.Reference{Id: users[handle+"@murphy.test"]}
		}
		return client.ScheduleInput{Name: name, TimeZone: &zone, Teams: &owners, Layers: []client.Layer{{Name: new("rotation"), TurnLengthSeconds: turn, StartsAt: starts, Participants: participants}}}
	}
	at := func(hour int) time.Time { return time.Date(2026, 1, 5, hour, 0, 0, 0, time.UTC) }
	everyTeam := []string{"platform", "payments", "mobile", "web", "data", "sre", "security"}
	schedules = append(schedules,
		rotation("mobile primary", "Europe/Lisbon", week, at(9), []string{"mobile"}, []string{"fay", "gus", "hana", "ivo", "jade", "xena"}),
		rotation("mobile secondary", "Europe/Lisbon", week, at(9), []string{"mobile"}, []string{"xena", "jade", "ivo", "hana", "gus", "fay"}),
		rotation("web primary", "Europe/Berlin", day, at(8), []string{"web"}, []string{"marco", "nia", "omar", "priya", "quinn", "rafa", "vera"}),
		rotation("web secondary", "Europe/Berlin", week, at(8), []string{"web"}, []string{"rafa", "quinn", "priya", "omar", "nia", "marco"}),
		rotation("data on-call", "Asia/Seoul", week, at(1), []string{"data"}, []string{"tom", "uma", "vic", "wen", "will"}),
		rotation("sre primary", "Europe/London", day, at(9), []string{"sre"}, []string{"zoe", "abe", "bea", "cal", "dina", "emil", "finn", "yuri"}),
		rotation("sre secondary", "Europe/London", week, at(9), []string{"sre"}, []string{"yuri", "finn", "emil", "dina", "cal", "bea", "abe", "zoe"}),
		rotation("security on-call", "Europe/Rome", week, at(8), []string{"security"}, []string{"hugo", "iris", "jon", "kira"}),
		rotation("engineering leads", "UTC", week, at(9), everyTeam, []string{"ana", "dee", "eli", "lena", "sara", "yara", "gia"}),
		rotation("platform weekend", "Europe/Malta", day, time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC), []string{"platform", "sre"}, []string{"mia", "noah", "olga", "pete", "zoe"}),
	)

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
