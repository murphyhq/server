//go:build load

package load

import (
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/testenv"
)

const pageDelayP99 = 5 * time.Second

func TestEveryIncidentPagesItsFirstLevel(t *testing.T) {
	p := profile(t)
	murphy := api(t)
	ctx := t.Context()
	run := uuid.NewV4().String()[:8]

	team, err := murphy.CreateTeamWithResponse(ctx, nil, client.TeamInput{Name: "load-escalation-" + run})
	require.NoError(t, err, "create the team")
	require.Equal(t, http.StatusCreated, team.StatusCode(), "create the team: %s", team.Body)
	user, err := murphy.CreateUserWithResponse(ctx, nil, client.UserInput{Name: "Load " + run, Email: "load-escalation-" + run + "@example.com"})
	require.NoError(t, err, "create the user")
	require.Equal(t, http.StatusCreated, user.StatusCode(), "create the user: %s", user.Body)
	policy, err := murphy.CreateEscalationPolicyWithResponse(ctx, nil, client.EscalationPolicyInput{
		Name:   "load-escalation-" + run,
		TeamId: team.JSON201.Id,
		Levels: []client.EscalationLevel{{DelaySeconds: 3600, Targets: []client.EscalationTarget{{Type: client.EscalationTargetTypeUser, Id: user.JSON201.Id}}}},
	})
	require.NoError(t, err, "create the policy")
	require.Equal(t, http.StatusCreated, policy.StatusCode(), "create the policy: %s", policy.Body)
	service, err := murphy.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "load-escalation-" + run, EscalationPolicyId: policy.JSON201.Id})
	require.NoError(t, err, "create the service")
	require.Equal(t, http.StatusCreated, service.StatusCode(), "create the service: %s", service.Body)
	integration, err := murphy.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: "generic", Type: client.Generic})
	require.NoError(t, err, "create the integration")
	require.Equal(t, http.StatusCreated, integration.StatusCode(), "create the integration: %s", integration.Body)

	var hits atomic.Uint64
	targeter := func(target *vegeta.Target) error {
		webhook := testenv.Generic(fmt.Sprintf("%s-%d", run, hits.Add(1)), "firing")
		target.Method = http.MethodPost
		target.URL = ingestURL + webhook.Path
		target.Body = webhook.Body
		target.Header = http.Header{"Content-Type": {webhook.ContentType}, "Authorization": {"Bearer " + integration.JSON201.Key}}
		return nil
	}
	attack(t, p, "escalation intake", targeter, Thresholds{Success: 1, P99: 500 * time.Millisecond}, nil)

	var delays []time.Duration
	require.Eventually(t, func() bool {
		var unpaged int
		delays, unpaged = pageDelays(t, murphy, service.JSON201.Id)
		return unpaged == 0 && len(delays) > 0
	}, p.Settle, time.Second, "every incident pages its first level")

	slices.Sort(delays)
	p50, p90, p99 := percentile(delays, 0.5), percentile(delays, 0.9), percentile(delays, 0.99)
	t.Logf("time from opening to the first page, over %d incidents: p50 %s, p90 %s, p99 %s", len(delays), p50, p90, p99)
	assert.LessOrEqual(t, p99, pageDelayP99, "p99 time from opening to the first page")
}

func pageDelays(t *testing.T, murphy *client.ClientWithResponses, serviceID string) (delays []time.Duration, unpaged int) {
	t.Helper()
	ids := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range ids {
				got, err := murphy.GetIncidentWithResponse(t.Context(), id)
				if err != nil || got.StatusCode() != http.StatusOK {
					mu.Lock()
					unpaged++
					mu.Unlock()
					continue
				}
				delay, paged := firstPage(got.JSON200)
				mu.Lock()
				if paged {
					delays = append(delays, delay)
				} else {
					unpaged++
				}
				mu.Unlock()
			}
		}()
	}
	limit := 100
	params := &client.ListIncidentsParams{ServiceId: &[]string{serviceID}, Limit: &limit}
	for {
		page, err := murphy.ListIncidentsWithResponse(t.Context(), params)
		if err != nil || page.StatusCode() != http.StatusOK {
			break
		}
		for _, incident := range page.JSON200.Items {
			ids <- incident.Id
		}
		if page.JSON200.NextCursor == "" {
			break
		}
		params.Cursor = &page.JSON200.NextCursor
	}
	close(ids)
	wg.Wait()
	return delays, unpaged
}

func firstPage(incident *client.IncidentWithTimeline) (time.Duration, bool) {
	for _, entry := range incident.Timeline {
		if entry.Type == client.TimelineEntryTypeEscalated {
			return entry.At.Sub(incident.CreatedAt), true
		}
	}
	return 0, false
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min(len(sorted)-1, int(float64(len(sorted))*p))]
}
