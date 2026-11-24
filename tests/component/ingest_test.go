//go:build integration

package component

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/services/catalog"
	"github.com/murphyhq/server/tests/murphytest"
	"github.com/murphyhq/server/tests/testenv"
)

type ingest struct {
	m        *murphytest.Murphy
	key      string
	received *testenv.Collected
}

func startIngest(t *testing.T, env brokertest.Environment) ingest {
	t.Helper()
	bus := testenv.NewBus(t, env, messaging.Topology{"engine": {"murphy.v1.alert": {"received"}}})
	keys, err := catalog.ParseKeyring(testenv.IntegrationKeySecrets)
	require.NoError(t, err)
	key, err := keys.Sign(&murphy.Integration{Id: murphy.NewID(), ServiceId: murphy.NewID(), KeyVersion: 1})
	require.NoError(t, err)
	return ingest{
		m:        murphytest.New(t, murphytest.At("", testenv.Ingest(t, bus.TopicURL))),
		key:      key,
		received: bus.Collect("engine", murphy.AlertVerb_ALERT_VERB_RECEIVED),
	}
}

func basic(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

var firing = murphytest.Payload{
	Path: "/v1/alerts", ContentType: "application/json",
	Body: []byte(`{"dedup_key": "disk-full", "status": "firing", "summary": "Disk almost full", "labels": {"host": "db-1"}}`),
}

func TestIngestPublishesAcceptedAlerts(t *testing.T) {
	for name, broker := range brokers {
		for scheme, authorization := range map[string]func(key string) string{
			"bearer": func(key string) string { return "Bearer " + key },
			"basic":  func(key string) string { return basic("murphy", key) },
		} {
			t.Run(name+"/"+scheme, func(t *testing.T) {
				ingest := startIngest(t, broker(t))

				status, body := ingest.m.SendWithAuthorization(firing, authorization(ingest.key))

				require.Equal(t, http.StatusAccepted, status)
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Equal(c, []string{body["id"].(string)}, ingest.received.IDs())
				}, murphytest.Settle, tick)
				published := ingest.received.Envelopes()[0]
				alert := published.GetAlert().GetCurrent()
				assert.Equal(t, murphy.AlertVerb_ALERT_VERB_RECEIVED, published.GetAlert().GetVerb())
				assert.Equal(t, "disk-full", alert.GetDedupKey())
				assert.Equal(t, murphy.AlertStatus_ALERT_STATUS_FIRING, alert.GetStatus())
				assert.Equal(t, "Disk almost full", alert.GetSummary())
				assert.Equal(t, map[string]string{"host": "db-1"}, alert.GetLabels())
				assert.NotEmpty(t, alert.GetIntegrationId())
				assert.NotEmpty(t, alert.GetServiceId())
			})
		}
	}
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

	accepted, _ := ingest.m.SendWithAuthorization(firing, "Bearer "+ingest.key)
	refused, _ := ingest.m.SendWithAuthorization(firing, basic("admin", ingest.key))

	require.Equal(t, http.StatusAccepted, accepted)
	require.Equal(t, http.StatusUnauthorized, refused)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 2, strings.Count(logs.String(), "request handled"))
	}, murphytest.Settle, tick)
	assert.NotContains(t, logs.String(), ingest.key)
	require.NotEmpty(t, spans.GetSpans())
	for _, span := range spans.GetSpans() {
		for _, attribute := range span.Attributes {
			assert.NotContains(t, attribute.Value.String(), ingest.key, span.Name)
		}
	}
}
