package metrics

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/murphyhq/server/internal/telemetry"
)

const prefix = "murphy."

type Dimension struct {
	Name  string
	Value string
}

type Counter struct {
	counter metric.Int64Counter
}

func NewCounter(name, description string) (*Counter, error) {
	counter, err := otel.Meter(telemetry.CallerOf(1).Package).Int64Counter(prefixed(name), metric.WithDescription(description), metric.WithUnit("1"))
	if err != nil {
		return nil, fmt.Errorf("create counter %s: %w", name, err)
	}
	return &Counter{counter: counter}, nil
}

func (c *Counter) Add(ctx context.Context, value int64, dimensions ...Dimension) {
	c.counter.Add(ctx, value, metric.WithAttributes(attributes(ctx, dimensions)...))
}

type Histogram struct {
	histogram metric.Float64Histogram
}

var secondsBuckets = []float64{0, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

func NewHistogram(name, description, unit string) (*Histogram, error) {
	options := []metric.Float64HistogramOption{metric.WithDescription(description), metric.WithUnit(unit)}
	if unit == "s" {
		options = append(options, metric.WithExplicitBucketBoundaries(secondsBuckets...))
	}
	histogram, err := otel.Meter(telemetry.CallerOf(1).Package).Float64Histogram(prefixed(name), options...)
	if err != nil {
		return nil, fmt.Errorf("create histogram %s: %w", name, err)
	}
	return &Histogram{histogram: histogram}, nil
}

func (h *Histogram) Record(ctx context.Context, value float64, dimensions ...Dimension) {
	h.histogram.Record(ctx, value, metric.WithAttributes(attributes(ctx, dimensions)...))
}

func attributes(ctx context.Context, dimensions []Dimension) []attribute.KeyValue {
	kvs := make([]attribute.KeyValue, 0, len(dimensions)+1)
	for _, d := range dimensions {
		kvs = append(kvs, attribute.String(prefixed(d.Name), d.Value))
	}
	if component := telemetry.Component(ctx); component != "" {
		kvs = append(kvs, attribute.String(prefixed("component"), component))
	}
	return kvs
}

func prefixed(name string) string {
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}
