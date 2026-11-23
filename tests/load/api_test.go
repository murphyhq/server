//go:build load

package load

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/client"
)

func TestTheAPIAnswersReadsQuickly(t *testing.T) {
	p := profile(t)
	murphy := api(t)
	ctx := t.Context()
	run := uuid.NewV4().String()[:8]

	team, err := murphy.CreateTeamWithResponse(ctx, nil, client.TeamInput{Name: "load-" + run})
	require.NoError(t, err, "create the team")
	require.Equal(t, http.StatusCreated, team.StatusCode(), "create the team: %s", team.Body)
	user, err := murphy.CreateUserWithResponse(ctx, nil, client.UserInput{Name: "Load " + run, Email: "load-" + run + "@example.com"})
	require.NoError(t, err, "create the user")
	require.Equal(t, http.StatusCreated, user.StatusCode(), "create the user: %s", user.Body)
	schedule, err := murphy.CreateScheduleWithResponse(ctx, nil, client.ScheduleInput{
		Name:    "load-" + run,
		TeamIds: &[]string{team.JSON201.Id},
		Layers:  []client.Layer{{TurnLengthSeconds: 24 * 60 * 60, StartsAt: time.Now().Add(-time.Hour), Participants: []string{user.JSON201.Id}}},
	})
	require.NoError(t, err, "create the schedule")
	require.Equal(t, http.StatusCreated, schedule.StatusCode(), "create the schedule: %s", schedule.Body)
	policy, err := murphy.CreateEscalationPolicyWithResponse(ctx, nil, client.EscalationPolicyInput{
		Name:   "load-" + run,
		TeamId: team.JSON201.Id,
		Levels: []client.EscalationLevel{{DelaySeconds: 300, Targets: []client.EscalationTarget{{Type: client.EscalationTargetTypeSchedule, Id: schedule.JSON201.Id}}}},
	})
	require.NoError(t, err, "create the policy")
	require.Equal(t, http.StatusCreated, policy.StatusCode(), "create the policy: %s", policy.Body)
	service, err := murphy.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "load-" + run, EscalationPolicyId: policy.JSON201.Id})
	require.NoError(t, err, "create the service")
	require.Equal(t, http.StatusCreated, service.StatusCode(), "create the service: %s", service.Body)

	reads := []string{
		"/v1/services",
		"/v1/incidents?service_id=" + service.JSON201.Id,
		"/v1/schedules/" + schedule.JSON201.Id + "/on-call",
		"/v1/schedules?team_id=" + team.JSON201.Id,
		"/v1/escalation-policies?team_id=" + team.JSON201.Id,
		"/v1/teams/" + team.JSON201.Id + "/members",
	}
	var hits atomic.Uint64
	targeter := func(target *vegeta.Target) error {
		target.Method = http.MethodGet
		target.URL = apiURL + reads[hits.Add(1)%uint64(len(reads))]
		return nil
	}
	attack(t, p, "API reads", targeter, Thresholds{Success: 1, P99: 300 * time.Millisecond}, nil)
}
