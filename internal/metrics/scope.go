package metrics

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var perScopeBuckets = []float64{0, 1, 2, 3, 4, 5, 6, 8, 10, 15, 20, 30, 50, 100}

var registry = struct {
	sync.Mutex
	counters   map[string]string
	histograms map[string]metric.Int64Histogram
}{counters: map[string]string{}, histograms: map[string]metric.Int64Histogram{}}

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

type scopeKey struct{}

type Scope struct {
	mu     sync.Mutex
	counts map[string]int64
}

func StartScope(ctx context.Context) (context.Context, *Scope) {
	s := &Scope{counts: map[string]int64{}}
	return context.WithValue(ctx, scopeKey{}, s), s
}

func (s *Scope) Record(ctx context.Context, unit string, dimensions ...Dimension) {
	s.mu.Lock()
	counts := s.counts
	s.mu.Unlock()

	registry.Lock()
	defer registry.Unlock()
	for name, description := range registry.counters {
		key := prefixed(unit + "." + name)
		histogram, ok := registry.histograms[key]
		if !ok {
			var err error
			histogram, err = otel.Meter("github.com/murphyhq/server/internal/metrics").Int64Histogram(key,
				metric.WithDescription(fmt.Sprintf("%s per %s", description, unit)),
				metric.WithUnit("{count}"),
				metric.WithExplicitBucketBoundaries(perScopeBuckets...))
			if err != nil {
				continue
			}
			registry.histograms[key] = histogram
		}
		histogram.Record(ctx, counts[name], metric.WithAttributes(attributes(dimensions)...))
	}
}
