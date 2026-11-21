package tracer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/tracer"
)

func record(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return exporter
}

type service struct{}

func (*service) Acknowledge(ctx context.Context, fail bool) (err error) {
	_, span := tracer.Span(ctx)
	defer span.End(&err)
	if fail {
		return errors.New("incident not found")
	}
	return nil
}

func acknowledge(ctx context.Context) {
	_, span := tracer.Span(ctx)
	span.End(nil)
}

func attrs(span sdktrace.ReadOnlySpan) map[string]string {
	out := map[string]string{}
	for _, kv := range span.Attributes() {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

func TestSpansAreNamedAndScopedAfterTheCaller(t *testing.T) {
	spans := record(t)

	acknowledge(context.Background())
	_ = (&service{}).Acknowledge(context.Background(), false)

	got := spans.GetSpans().Snapshots()
	require.Len(t, got, 2)
	assert.Equal(t, "tracer_test.acknowledge", got[0].Name())
	assert.Equal(t, "tracer_test.service.Acknowledge", got[1].Name())
	assert.Equal(t, "github.com/murphyhq/server/internal/tracer_test", got[0].InstrumentationScope().Name)
}

func TestEndRecordsErrors(t *testing.T) {
	spans := record(t)

	_ = (&service{}).Acknowledge(context.Background(), true)
	_ = (&service{}).Acknowledge(context.Background(), false)

	got := spans.GetSpans().Snapshots()
	require.Len(t, got, 2)
	assert.Equal(t, codes.Error, got[0].Status().Code)
	assert.Equal(t, "incident not found", got[0].Status().Description)
	require.NotEmpty(t, got[0].Events())
	assert.Equal(t, "exception", got[0].Events()[0].Name)
	assert.Equal(t, codes.Unset, got[1].Status().Code)
}

func TestContextFieldsAppearOnEverySpan(t *testing.T) {
	spans := record(t)

	ctx := logger.With(context.Background(), logger.WithField("incident.id", "inc-1"))
	ctx, parent := tracer.Span(ctx)
	ctx = logger.With(ctx, logger.WithField("escalation.step", 2))
	_, child := tracer.Span(ctx)
	child.End(nil)
	parent.End(nil)

	got := spans.GetSpans().Snapshots()
	require.Len(t, got, 2)
	childAttrs, parentAttrs := attrs(got[0]), attrs(got[1])
	assert.Equal(t, "inc-1", parentAttrs["murphy.incident.id"])
	assert.Equal(t, "inc-1", childAttrs["murphy.incident.id"])
	assert.Equal(t, "2", childAttrs["murphy.escalation.step"])
	assert.NotContains(t, parentAttrs, "murphy.escalation.step")
}

func TestSetAddsFieldsToTheRunningSpanAndWhatFollows(t *testing.T) {
	spans := record(t)

	ctx, parent := tracer.Span(context.Background())
	ctx = parent.Set(ctx, logger.WithField("incident.id", "inc-1"))
	_, child := tracer.Span(ctx)
	child.End(nil)
	parent.End(nil)

	assert.Equal(t, "inc-1", logger.Fields(ctx)[0].Value)
	got := spans.GetSpans().Snapshots()
	require.Len(t, got, 2)
	assert.Equal(t, "inc-1", attrs(got[1])["murphy.incident.id"], "running span")
	assert.Equal(t, "inc-1", attrs(got[0])["murphy.incident.id"], "child span")
}
