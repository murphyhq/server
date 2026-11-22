//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/services/catalog"
	"github.com/murphyhq/server/tests/testenv"
)

type ingestFixture struct {
	url      string
	key      string
	received chan *murphy.Envelope
}

var brokers = map[string]func(t *testing.T) brokertest.Environment{
	"nats": func(*testing.T) brokertest.Environment { return testenv.NATS() },
	"sqs":  testenv.SQS,
}

func eachBroker(t *testing.T, test func(t *testing.T, ingest ingestFixture)) {
	for name, environment := range brokers {
		t.Run(name, func(t *testing.T) {
			test(t, startIngest(t, environment(t)))
		})
	}
}

func startIngest(t *testing.T, env brokertest.Environment) ingestFixture {
	t.Helper()
	ctx := t.Context()
	endpoints := env.Provision(t, brokertest.Topology{"engine": {"murphy.v1.alert"}}, messaging.TopologyVersion)

	subscription, err := broker.OpenSubscription(ctx, endpoints.Subscriptions["engine"])
	require.NoError(t, err)
	t.Cleanup(func() { _ = subscription.Close() })
	received := make(chan *murphy.Envelope, 10)
	consumer, err := messaging.Consume(ctx, subscription, "engine", messaging.Handlers{
		messaging.On(murphy.AlertVerb_ALERT_VERB_RECEIVED, func(_ context.Context, envelope *murphy.Envelope) error {
			received <- envelope
			return nil
		}),
	})
	require.NoError(t, err)
	t.Cleanup(consumer.Stop)

	keys, err := catalog.ParseKeyring(testenv.IntegrationKeySecrets)
	require.NoError(t, err)
	key, err := keys.Sign(&murphy.Integration{Id: murphy.NewID(), ServiceId: murphy.NewID(), KeyVersion: 1})
	require.NoError(t, err)

	return ingestFixture{url: testenv.Ingest(t, endpoints.Topic), key: key, received: received}
}

func postAlert(t *testing.T, url, authorization string, body any) (*http.Response, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/alerts", bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp, out
}

func basic(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

var firing = map[string]any{"dedup_key": "disk-full", "status": "firing", "summary": "Disk almost full", "labels": map[string]string{"host": "db-1"}}

func TestIngestPublishesAcceptedAlerts(t *testing.T) {
	eachBroker(t, func(t *testing.T, ingest ingestFixture) {
		for name, authorization := range map[string]string{
			"bearer": "Bearer " + ingest.key,
			"basic":  basic("murphy", ingest.key),
		} {
			t.Run(name, func(t *testing.T) {
				resp, body := postAlert(t, ingest.url, authorization, firing)
				require.Equal(t, http.StatusAccepted, resp.StatusCode)

				select {
				case envelope := <-ingest.received:
					assert.Equal(t, body["id"], envelope.GetId())
					alert := envelope.GetAlert().GetCurrent()
					assert.Equal(t, murphy.AlertVerb_ALERT_VERB_RECEIVED, envelope.GetAlert().GetVerb())
					assert.Equal(t, "disk-full", alert.GetDedupKey())
					assert.Equal(t, murphy.AlertStatus_ALERT_STATUS_FIRING, alert.GetStatus())
					assert.Equal(t, "Disk almost full", alert.GetSummary())
					assert.Equal(t, map[string]string{"host": "db-1"}, alert.GetLabels())
					assert.NotEmpty(t, alert.GetIntegrationId())
					assert.NotEmpty(t, alert.GetServiceId())
				case <-time.After(10 * time.Second):
					t.Fatal("no message published")
				}
			})
		}
	})
}

func TestIngestRefusesRequestsWithoutAValidKey(t *testing.T) {
	eachBroker(t, func(t *testing.T, ingest ingestFixture) {
		other, err := catalog.ParseKeyring("9:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
		require.NoError(t, err)
		forged, err := other.Sign(&murphy.Integration{Id: murphy.NewID(), ServiceId: murphy.NewID(), KeyVersion: 1})
		require.NoError(t, err)

		for name, authorization := range map[string]string{
			"missing":                "",
			"key without scheme":     ingest.key,
			"signed by another":      "Bearer " + forged,
			"basic with other user":  basic("admin", ingest.key),
			"basic with wrong value": basic("murphy", "nope"),
		} {
			t.Run(name, func(t *testing.T) {
				resp, body := postAlert(t, ingest.url, authorization, firing)

				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
				assert.Equal(t, "invalid integration key", body["detail"])
			})
		}
		assert.Empty(t, ingest.received)
	})
}

func TestIngestRefusesInvalidAlerts(t *testing.T) {
	eachBroker(t, func(t *testing.T, ingest ingestFixture) {

		for name, alert := range map[string]any{
			"no dedup key":   map[string]any{"status": "firing", "summary": "s"},
			"unknown status": map[string]any{"dedup_key": "k", "status": "burning", "summary": "s"},
			"no summary":     map[string]any{"dedup_key": "k", "status": "resolved"},
		} {
			t.Run(name, func(t *testing.T) {
				resp, _ := postAlert(t, ingest.url, "Bearer "+ingest.key, alert)
				assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			})
		}
		assert.Empty(t, ingest.received)
	})
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestIngestNeverRecordsTheKey(t *testing.T) {
	logs := &lockedBuffer{}
	flush, err := telemetry.Setup(t.Context(), "ingest", "debug", logs)
	require.NoError(t, err)
	t.Cleanup(func() { _ = flush(context.Background()) })
	spans := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	ingest := startIngest(t, testenv.NATS())
	accepted, _ := postAlert(t, ingest.url, "Bearer "+ingest.key, firing)
	require.Equal(t, http.StatusAccepted, accepted.StatusCode)
	refused, _ := postAlert(t, ingest.url, basic("admin", ingest.key), firing)
	require.Equal(t, http.StatusUnauthorized, refused.StatusCode)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 2, strings.Count(logs.String(), "request handled"))
	}, 5*time.Second, 50*time.Millisecond)
	assert.NotContains(t, logs.String(), ingest.key)
	require.NotEmpty(t, spans.GetSpans())
	for _, span := range spans.GetSpans() {
		for _, attribute := range span.Attributes {
			assert.NotContains(t, attribute.Value.Emit(), ingest.key, span.Name)
		}
	}
}
