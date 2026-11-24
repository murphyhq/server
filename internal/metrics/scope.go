package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	perScopeBuckets         = []float64{0, 1, 2, 3, 4, 5, 6, 8, 10, 15, 20, 30, 50, 100}
	perScopeDurationBuckets = []float64{0, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
)

var registry = struct {
	sync.Mutex
	counters  map[string]string
	durations map[string]string
	counts    map[string]metric.Int64Histogram
	timings   map[string]metric.Float64Histogram
}{
	counters:  map[string]string{},
	durations: map[string]string{},
	counts:    map[string]metric.Int64Histogram{},
	timings:   map[string]metric.Float64Histogram{},
}

type PerScopeCounter struct {
	name string
}

func NewPerScopeCounter(name, description string) *PerScopeCounter {
	registry.Lock()
	defer registry.Unlock()
	registry.counters[name] = description
	return &PerScopeCounter{name: name}
}

func (c *PerScopeCounter) Add(ctx context.Context) {
	if s, ok := ctx.Value(scopeKey{}).(*Scope); ok {
		s.mu.Lock()
		s.counts[c.name]++
		s.mu.Unlock()
	}
}

type PerScopeDuration struct {
	name string
}

func NewPerScopeDuration(name, description string) *PerScopeDuration {
	registry.Lock()
	defer registry.Unlock()
	registry.durations[name] = description
	return &PerScopeDuration{name: name}
}

func (d *PerScopeDuration) Add(ctx context.Context, elapsed time.Duration) {
	if s, ok := ctx.Value(scopeKey{}).(*Scope); ok {
		s.mu.Lock()
		s.durations[d.name] += elapsed.Seconds()
		s.mu.Unlock()
	}
}

type scopeKey struct{}

type Scope struct {
	mu        sync.Mutex
	counts    map[string]int64
	durations map[string]float64
}

func StartScope(ctx context.Context) (context.Context, *Scope) {
	s := &Scope{counts: map[string]int64{}, durations: map[string]float64{}}
	return context.WithValue(ctx, scopeKey{}, s), s
}

func (s *Scope) Record(ctx context.Context, unit string, dimensions ...Dimension) {
	s.mu.Lock()
	counts, durations := s.counts, s.durations
	s.mu.Unlock()
	attrs := metric.WithAttributes(attributes(ctx, dimensions)...)

	registry.Lock()
	defer registry.Unlock()
	meter := otel.Meter("github.com/murphyhq/server/internal/metrics")
	for name, description := range registry.counters {
		key := prefixed(unit + "." + name)
		histogram, ok := registry.counts[key]
		if !ok {
			var err error
			histogram, err = meter.Int64Histogram(key,
				metric.WithDescription(fmt.Sprintf("%s per %s", description, unit)),
				metric.WithUnit("{count}"),
				metric.WithExplicitBucketBoundaries(perScopeBuckets...))
			if err != nil {
				continue
			}
			registry.counts[key] = histogram
		}
		histogram.Record(ctx, counts[name], attrs)
	}
	for name, description := range registry.durations {
		key := prefixed(unit + "." + name)
		histogram, ok := registry.timings[key]
		if !ok {
			var err error
			histogram, err = meter.Float64Histogram(key,
				metric.WithDescription(fmt.Sprintf("%s per %s", description, unit)),
				metric.WithUnit("s"),
				metric.WithExplicitBucketBoundaries(perScopeDurationBuckets...))
			if err != nil {
				continue
			}
			registry.timings[key] = histogram
		}
		histogram.Record(ctx, durations[name], attrs)
	}
}
