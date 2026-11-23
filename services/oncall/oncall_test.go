package oncall

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
)

func malta(t *testing.T, value string) time.Time {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.ParseInLocation("2006-01-02 15:04", value, zone)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func layer(start time.Time, turn time.Duration, participants ...string) *murphy.Layer {
	return &murphy.Layer{Participants: participants, TurnLength: durationpb.New(turn), StartsAt: timestamppb.New(start)}
}

func schedule(layers ...*murphy.Layer) *murphy.Schedule {
	return &murphy.Schedule{Name: "primary", TimeZone: "Europe/Malta", Layers: layers}
}

func override(user string, start, end time.Time) *murphy.Override {
	return &murphy.Override{UserId: user, StartsAt: timestamppb.New(start), EndsAt: timestamppb.New(end)}
}

func TestParticipantsTakeTurns(t *testing.T) {
	start := malta(t, "2026-11-02 09:00")
	s := schedule(layer(start, 8*time.Hour, "ana", "bruno", "carla"))

	for at, want := range map[time.Time]string{
		start.Add(-time.Minute):                  "",
		start:                                    "ana",
		start.Add(8*time.Hour - time.Nanosecond): "ana",
		start.Add(8 * time.Hour):                 "bruno",
		start.Add(16 * time.Hour):                "carla",
		start.Add(24 * time.Hour):                "ana",
	} {
		assert.Equal(t, want, onCall(s, at, nil), at.String())
	}
}

func TestDailyHandoffsKeepTheirTimeAcrossDaylightSaving(t *testing.T) {
	s := schedule(layer(malta(t, "2026-03-27 09:00"), 24*time.Hour, "ana", "bruno"))

	assert.Equal(t, "ana", onCall(s, malta(t, "2026-03-27 09:00"), nil))
	assert.Equal(t, "bruno", onCall(s, malta(t, "2026-03-28 09:00"), nil))
	assert.Equal(t, "bruno", onCall(s, malta(t, "2026-03-29 08:59"), nil), "the clocks went forward overnight; the handoff is still at 09:00")
	assert.Equal(t, "ana", onCall(s, malta(t, "2026-03-29 09:00"), nil))
	assert.Equal(t, "bruno", onCall(s, malta(t, "2026-03-30 09:00"), nil))
}

func TestWeeklyHandoffsKeepTheirTimeAcrossDaylightSaving(t *testing.T) {
	s := schedule(layer(malta(t, "2026-10-19 09:00"), 7*24*time.Hour, "ana", "bruno"))

	assert.Equal(t, "ana", onCall(s, malta(t, "2026-10-26 08:59"), nil), "the clocks went back during the week; the handoff is still Monday 09:00")
	assert.Equal(t, "bruno", onCall(s, malta(t, "2026-10-26 09:00"), nil))
}

func TestLaterLayersTakePrecedenceOnceTheyStart(t *testing.T) {
	start := malta(t, "2026-11-02 09:00")
	s := schedule(
		layer(start, 24*time.Hour, "ana"),
		layer(start.Add(48*time.Hour), 24*time.Hour, "bruno"),
	)

	assert.Equal(t, "ana", onCall(s, start.Add(24*time.Hour), nil))
	assert.Equal(t, "bruno", onCall(s, start.Add(48*time.Hour), nil))
}

func TestOverridesReplaceTheRotationForTheirTime(t *testing.T) {
	start := malta(t, "2026-11-02 09:00")
	s := schedule(layer(start, 24*time.Hour, "ana"))
	overrides := []*murphy.Override{
		override("bruno", start.Add(2*time.Hour), start.Add(6*time.Hour)),
		override("carla", start.Add(4*time.Hour), start.Add(5*time.Hour)),
	}

	for at, want := range map[time.Time]string{
		start.Add(time.Hour):                     "ana",
		start.Add(2 * time.Hour):                 "bruno",
		start.Add(4 * time.Hour):                 "carla",
		start.Add(5 * time.Hour):                 "bruno",
		start.Add(6*time.Hour - time.Nanosecond): "bruno",
		start.Add(6 * time.Hour):                 "ana",
	} {
		assert.Equal(t, want, onCall(s, at, overrides), at.String())
	}
}
