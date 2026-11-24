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
	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/tests/murphytest"
)

type source struct {
	integration client.IntegrationType
	firing      func(key string) murphytest.Payload
}

var sources = []source{
	{client.Alertmanager, func(key string) murphytest.Payload {
		return murphytest.Alertmanager(key, murphytest.GroupedAlert{Fingerprint: key, Status: "firing", StartsAt: time.Now(), Summary: "Disk almost full"})
	}},
	{client.Grafana, func(key string) murphytest.Payload {
		return murphytest.Grafana(key, murphytest.GroupedAlert{Fingerprint: key, Status: "firing", StartsAt: time.Now(), Summary: "Disk almost full"})
	}},
	{client.Datadog, func(key string) murphytest.Payload { return murphytest.Datadog(key, key, "Triggered") }},
	{client.Cloudwatch, func(key string) murphytest.Payload {
		return murphytest.CloudWatch("arn:aws:cloudwatch:eu-west-1:123456789012:alarm:"+key, "ALARM")
	}},
	{client.GoogleCloud, func(key string) murphytest.Payload { return murphytest.GoogleCloud(key, "open") }},
	{client.Generic, func(key string) murphytest.Payload { return murphytest.Generic(key, "firing") }},
}

type feed struct {
	source
	service *murphytest.Service
}

func TestEveryAlertBecomesAnIncident(t *testing.T) {
	p := profile(t)
	m := murphytest.New(t)
	run := uuid.NewV4().String()[:8]
	feeds := make([]*feed, len(sources))
	for i, s := range sources {
		feeds[i] = &feed{source: s, service: m.Service(murphytest.Receives(s.integration), murphytest.Pages(m.User(murphytest.WithWebhook()).Target()))}
	}
	var hits atomic.Uint64
	targeter := func(target *vegeta.Target) error {
		n := hits.Add(1)
		f := feeds[n%uint64(len(feeds))]
		payload := f.firing(fmt.Sprintf("%s-%d", run, n))
		target.Method = http.MethodPost
		target.URL = ingestURL + payload.Path
		target.Body = payload.Body
		target.Header = http.Header{"Content-Type": {payload.ContentType}, "Authorization": {"Bearer " + f.service.Key()}}
		return nil
	}
	accepted := acceptedByPath()

	attack(t, p, "alert intake", targeter, Thresholds{Success: 1, P99: 500 * time.Millisecond}, accepted.observe)

	for _, f := range feeds {
		expected := accepted.count(f.firing("").Path)
		var opened int
		var lastErr error
		assert.Eventually(t, func() bool {
			opened, lastErr = incidents(m.API, f.service.ID)
			return lastErr == nil && opened == expected
		}, p.Settle, time.Second, "%s: %d alerts accepted, %d incidents opened, last error: %v", f.integration, expected, opened, lastErr)
	}
}
