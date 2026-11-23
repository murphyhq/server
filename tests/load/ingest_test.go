//go:build load

package load

import (
	"fmt"
	"net/http"
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

type source struct {
	integration client.IntegrationType
	firing      func(key string) testenv.Webhook
}

var sources = []source{
	{client.Alertmanager, func(key string) testenv.Webhook {
		return testenv.Alertmanager(key, testenv.GroupedAlert{Fingerprint: key, Status: "firing", StartsAt: time.Now(), Summary: "Disk almost full"})
	}},
	{client.Grafana, func(key string) testenv.Webhook {
		return testenv.Grafana(key, testenv.GroupedAlert{Fingerprint: key, Status: "firing", StartsAt: time.Now(), Summary: "Disk almost full"})
	}},
	{client.Datadog, func(key string) testenv.Webhook { return testenv.Datadog(key, key, "Triggered") }},
	{client.Cloudwatch, func(key string) testenv.Webhook {
		return testenv.CloudWatch("arn:aws:cloudwatch:eu-west-1:123456789012:alarm:"+key, "ALARM")
	}},
	{client.GoogleCloud, func(key string) testenv.Webhook { return testenv.GoogleCloud(key, "open") }},
	{client.Generic, func(key string) testenv.Webhook { return testenv.Generic(key, "firing") }},
}

type feed struct {
	source
	serviceID string
	key       string
	sent      atomic.Int64
}

func TestEveryAlertBecomesAnIncident(t *testing.T) {
	p := profile(t)
	murphy := api(t)
	run := uuid.NewV4().String()[:8]

	feeds := make([]*feed, len(sources))
	for i, s := range sources {
		feeds[i] = &feed{source: s}
		feeds[i].serviceID, feeds[i].key = integration(t, murphy, fmt.Sprintf("load-%s-%s", run, s.integration), s.integration)
	}

	var hits atomic.Uint64
	targeter := func(target *vegeta.Target) error {
		n := hits.Add(1)
		f := feeds[n%uint64(len(feeds))]
		webhook := f.firing(fmt.Sprintf("%s-%d", run, n))
		target.Method = http.MethodPost
		target.URL = ingestURL + webhook.Path
		target.Body = webhook.Body
		target.Header = http.Header{"Content-Type": {webhook.ContentType}, "Authorization": {"Bearer " + f.key}}
		f.sent.Add(1)
		return nil
	}
	attack(t, p, "alert intake", targeter, Thresholds{Success: 1, P99: 500 * time.Millisecond})

	for _, f := range feeds {
		want := int(f.sent.Load())
		assert.Eventually(t, func() bool { return incidents(t, murphy, f.serviceID) == want }, p.Settle, time.Second,
			"%s: %d alerts sent, %d incidents opened", f.integration, want, incidents(t, murphy, f.serviceID))
	}
}

func integration(t *testing.T, murphy *client.ClientWithResponses, name string, kind client.IntegrationType) (string, string) {
	t.Helper()
	service, err := murphy.CreateServiceWithResponse(t.Context(), nil, client.ServiceInput{Name: name, EscalationPolicyId: uuid.NewV7().String()})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, service.StatusCode())
	created, err := murphy.CreateIntegrationWithResponse(t.Context(), service.JSON201.Id, nil, client.IntegrationCreate{Name: name, Type: kind})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, created.StatusCode())
	return service.JSON201.Id, created.JSON201.Key
}

func incidents(t *testing.T, murphy *client.ClientWithResponses, serviceID string) int {
	t.Helper()
	limit := 100
	params := &client.ListIncidentsParams{ServiceId: &[]string{serviceID}, Limit: &limit}
	count := 0
	for {
		page, err := murphy.ListIncidentsWithResponse(t.Context(), params)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, page.StatusCode())
		count += len(page.JSON200.Items)
		if page.JSON200.NextCursor == "" {
			return count
		}
		params.Cursor = &page.JSON200.NextCursor
	}
}
