//go:build load

package load

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/tests/murphytest"
)

func TestTheAPIAnswersReadsQuickly(t *testing.T) {
	p := profile(t)
	m := murphytest.New(t)
	team := m.Team()
	schedule := m.Schedule(team, m.User())
	service := m.Service(murphytest.OwnedBy(team), murphytest.Pages(schedule.Target()))
	reads := []string{
		"/v1/services",
		"/v1/incidents?service_id=" + service.ID,
		"/v1/schedules/" + schedule.ID + "/on-call",
		"/v1/schedules?team_id=" + team.ID,
		"/v1/escalation-policies?team_id=" + team.ID,
		"/v1/teams/" + team.ID + "/members",
	}
	var hits atomic.Uint64
	targeter := func(target *vegeta.Target) error {
		target.Method = http.MethodGet
		target.URL = m.APIURL() + reads[hits.Add(1)%uint64(len(reads))]
		target.Header = http.Header{"Authorization": {"Bearer " + m.Session()}}
		return nil
	}

	attack(t, p, "API reads", targeter, Thresholds{Success: 1, P99: 300 * time.Millisecond}, nil)
}
