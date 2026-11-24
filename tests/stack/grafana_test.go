//go:build stack

package stack

import (
	"testing"
	"time"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/murphytest"
)

func TestGrafanaPagesMurphyAndResolvesTheIncident(t *testing.T) {
	m := murphytest.New(t, murphytest.Patience(3*time.Minute))
	service := m.Service(murphytest.Receives(client.Grafana))
	rule := m.GrafanaRuleFor(service)

	rule.Fire()
	incident := service.WaitForIncident()
	rule.Recover()

	incident.WaitFor(murphytest.Status(client.IncidentStatusResolved))
}
