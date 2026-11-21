package metrics_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/murphyhq/server/internal/metrics"
	"github.com/murphyhq/server/internal/telemetry"
)

type collected struct {
	data  metricdata.Aggregation
	scope string
}

func collect(t *testing.T) func() map[string]collected {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(previous) })
	return func() map[string]collected {
		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))
		out := map[string]collected{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				out[m.Name] = collected{data: m.Data, scope: sm.Scope.Name}
			}
		}
		return out
	}
}

func TestCounterRecordsWithItsDimensionsOnly(t *testing.T) {
	read := collect(t)
	counter, err := metrics.NewCounter("pages.sent", "Pages sent")
	require.NoError(t, err)

	ctx := telemetry.With(context.Background(), telemetry.WithField("incident.id", "inc-1"))
	counter.Add(ctx, 2, metrics.Dimension{Name: "channel", Value: "sms"})

	got := read()["murphy.pages.sent"]
	assert.Equal(t, "github.com/murphyhq/server/internal/metrics_test", got.scope)
	sum, ok := got.data.(metricdata.Sum[int64])
	require.True(t, ok, "murphy.pages.sent is not a counter: %+v", got)
	require.Len(t, sum.DataPoints, 1)

	point := sum.DataPoints[0]
	assert.Equal(t, int64(2), point.Value)
	channel, ok := point.Attributes.Value("murphy.channel")
	assert.True(t, ok)
	assert.Equal(t, "sms", channel.AsString())
	_, leaked := point.Attributes.Value("murphy.incident.id")
	assert.False(t, leaked, "context field leaked into a metric dimension")
}

func TestHistogramRecordsValues(t *testing.T) {
	read := collect(t)
	histogram, err := metrics.NewHistogram("page.latency", "Time from alert to first page", "s")
	require.NoError(t, err)

	histogram.Record(context.Background(), 1.5, metrics.Dimension{Name: "channel", Value: "sms"})

	h, ok := read()["murphy.page.latency"].data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, h.DataPoints, 1)
	assert.Equal(t, uint64(1), h.DataPoints[0].Count)
	assert.Equal(t, 1.5, h.DataPoints[0].Sum)
}
