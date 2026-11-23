//go:build load

package load

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
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
	accepted  int
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
		return nil
	}
	byPath := map[string]*feed{}
	for _, f := range feeds {
		byPath[f.firing("").Path] = f
	}
	accept := func(result *vegeta.Result) {
		if result.Code != http.StatusAccepted {
			return
		}
		if u, err := neturl.Parse(result.URL); err == nil && byPath[u.Path] != nil {
			byPath[u.Path].accepted++
		}
	}
	attack(t, p, "alert intake", targeter, Thresholds{Success: 1, P99: 500 * time.Millisecond}, accept)

	for _, f := range feeds {
		var opened int
		var lastErr error
		assert.Eventually(t, func() bool {
			opened, lastErr = incidents(murphy, f.serviceID)
			return lastErr == nil && opened == f.accepted
		}, p.Settle, time.Second, "%s: %d alerts accepted, %d incidents opened, last error: %v", f.integration, f.accepted, opened, lastErr)
	}
}

func integration(t *testing.T, murphy *client.ClientWithResponses, name string, kind client.IntegrationType) (string, string) {
	t.Helper()
	policyID, _ := escalationPolicy(t, murphy, name)
	service, err := murphy.CreateServiceWithResponse(t.Context(), nil, client.ServiceInput{Name: name, EscalationPolicy: client.Reference{Id: policyID}})
	require.NoError(t, err, "create service %s", name)
	require.Equal(t, http.StatusCreated, service.StatusCode(), "create service %s: %s", name, service.Body)
	created, err := murphy.CreateIntegrationWithResponse(t.Context(), service.JSON201.Id, nil, client.IntegrationCreate{Name: name, Type: kind})
	require.NoError(t, err, "create integration %s", name)
	require.Equal(t, http.StatusCreated, created.StatusCode(), "create integration %s: %s", name, created.Body)
	return service.JSON201.Id, created.JSON201.Key
}

func incidents(murphy *client.ClientWithResponses, serviceID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	limit := 100
	params := &client.ListIncidentsParams{ServiceId: &[]string{serviceID}, Limit: &limit}
	count := 0
	for {
		page, err := murphy.ListIncidentsWithResponse(ctx, params)
		if err != nil {
			return count, fmt.Errorf("list incidents: %w", err)
		}
		if page.StatusCode() != http.StatusOK {
			return count, fmt.Errorf("list incidents: %s", page.Status())
		}
		count += len(page.JSON200.Items)
		if page.JSON200.NextCursor == "" {
			return count, nil
		}
		params.Cursor = &page.JSON200.NextCursor
	}
}
