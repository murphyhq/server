//go:build load

package load

import (
	"fmt"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/tests/murphytest"
)

const pageDelayP99 = 5 * time.Second

func TestEveryIncidentPagesItsFirstLevel(t *testing.T) {
	p := profile(t)
	m := murphytest.New(t)
	run := uuid.NewV4().String()[:8]
	service := m.Service(murphytest.Pages(m.User(murphytest.WithWebhook()).Target()))

	var hits atomic.Uint64
	targeter := func(target *vegeta.Target) error {
		payload := murphytest.Generic(fmt.Sprintf("%s-%d", run, hits.Add(1)), "firing")
		target.Method = http.MethodPost
		target.URL = ingestURL + payload.Path
		target.Body = payload.Body
		target.Header = http.Header{"Content-Type": {payload.ContentType}, "Authorization": {"Bearer " + service.Key()}}
		return nil
	}
	attack(t, p, "escalation intake", targeter, Thresholds{Success: 1, P99: 500 * time.Millisecond}, nil)

	var delays []time.Duration
	require.Eventually(t, func() bool {
		var unpaged int
		delays, unpaged = pageDelays(t, m.API, service.ID)
		return unpaged == 0 && len(delays) > 0
	}, p.Settle, time.Second, "every incident's first level gets its webhook")

	slices.Sort(delays)
	p50, p90, p99 := percentile(delays, 0.5), percentile(delays, 0.9), percentile(delays, 0.99)
	t.Logf("time from opening to the first webhook sent, over %d incidents: p50 %s, p90 %s, p99 %s", len(delays), p50, p90, p99)
	assert.LessOrEqual(t, p99, pageDelayP99, "p99 time from opening to the first webhook sent")
}
