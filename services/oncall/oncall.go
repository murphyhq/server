package oncall

import (
	"time"

	"github.com/murphyhq/server"
)

const day = 24 * time.Hour

func onCall(schedule *murphy.Schedule, at time.Time, overrides []*murphy.Override) string {
	var winner *murphy.Override
	for _, override := range overrides {
		if covers(override, at) && (winner == nil || override.GetStartsAt().AsTime().After(winner.GetStartsAt().AsTime())) {
			winner = override
		}
	}
	if winner != nil {
		return winner.GetUserId()
	}

	zone, err := time.LoadLocation(schedule.GetTimeZone())
	if err != nil {
		zone = time.UTC
	}
	layers := schedule.GetLayers()
	for i := len(layers) - 1; i >= 0; i-- {
		if user := layerOnCall(layers[i], at, zone); user != "" {
			return user
		}
	}
	return ""
}

func covers(override *murphy.Override, at time.Time) bool {
	return !at.Before(override.GetStartsAt().AsTime()) && at.Before(override.GetEndsAt().AsTime())
}

func layerOnCall(layer *murphy.Layer, at time.Time, zone *time.Location) string {
	participants := layer.GetParticipants()
	turn := layer.GetTurnLength().AsDuration()
	start := layer.GetStartsAt().AsTime()
	if len(participants) == 0 || turn <= 0 || at.Before(start) {
		return ""
	}
	return participants[turns(start.In(zone), at.In(zone), turn)%int64(len(participants))]
}

func turns(start, at time.Time, turn time.Duration) int64 {
	if turn%day != 0 {
		return int64(at.Sub(start) / turn)
	}
	days := int(at.Sub(start) / day)
	for start.AddDate(0, 0, days).After(at) {
		days--
	}
	for !start.AddDate(0, 0, days+1).After(at) {
		days++
	}
	return int64(days) / int64(turn/day)
}
