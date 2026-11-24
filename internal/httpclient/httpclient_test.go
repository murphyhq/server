package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/murphyhq/server/internal/httpclient"
	"github.com/murphyhq/server/internal/telemetry"
)

func TestOutgoingCallsAreMeasuredUnderTheirComponent(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(previous) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	request, err := http.NewRequestWithContext(telemetry.WithComponent(t.Context(), "notifier"), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	response, err := httpclient.New(time.Second).Do(request)
	require.NoError(t, err)
	_ = response.Body.Close()

	components := componentsMeasured(t, reader)
	assert.Equal(t, []string{"notifier"}, components)
}

func componentsMeasured(t *testing.T, reader *sdkmetric.ManualReader) []string {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))
	var components []string
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || m.Name != "http.client.request.duration" {
				continue
			}
			for _, point := range histogram.DataPoints {
				component, _ := point.Attributes.Value("murphy.component")
				components = append(components, component.AsString())
			}
		}
	}
	return components
}
