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
	c.counter.Add(ctx, value, metric.WithAttributes(attributes(dimensions)...))
}

type Histogram struct {
	histogram metric.Float64Histogram
}

func NewHistogram(name, description, unit string) (*Histogram, error) {
	histogram, err := otel.Meter(telemetry.CallerOf(1).Package).Float64Histogram(prefixed(name), metric.WithDescription(description), metric.WithUnit(unit))
	if err != nil {
		return nil, fmt.Errorf("create histogram %s: %w", name, err)
	}
	return &Histogram{histogram: histogram}, nil
}

func (h *Histogram) Record(ctx context.Context, value float64, dimensions ...Dimension) {
	h.histogram.Record(ctx, value, metric.WithAttributes(attributes(dimensions)...))
}

func attributes(dimensions []Dimension) []attribute.KeyValue {
	kvs := make([]attribute.KeyValue, len(dimensions))
	for i, d := range dimensions {
		kvs[i] = attribute.String(prefixed(d.Name), d.Value)
	}
	return kvs
}

func prefixed(name string) string {
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}
