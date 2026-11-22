package messaging_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
)

func traces(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return exporter
}

func alertReceived(id string) *murphy.Envelope {
	return &murphy.Envelope{
		Id: id,
		Event: &murphy.Envelope_Alert{Alert: &murphy.AlertEvent{
			Verb:    murphy.AlertVerb_ALERT_VERB_RECEIVED,
			Current: &murphy.Alert{Id: "alert-1", DedupKey: "disk-full"},
		}},
	}
}

func newBroker(t *testing.T) *MockBroker {
	broker := NewMockBroker(t)
	broker.EXPECT().System().Return("test").Maybe()
	return broker
}

func subscribed(t *testing.T, broker *MockBroker, handle messaging.Handler) func(context.Context, messaging.Delivery) error {
	t.Helper()
	var deliver func(context.Context, messaging.Delivery) error
	broker.EXPECT().Subscribe(mock.Anything, "engine", []string{"murphy.v1.alert"}, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ []string, handle func(context.Context, messaging.Delivery) error) (messaging.Subscription, error) {
			deliver = handle
			return NewMockSubscription(t), nil
		})
	_, err := messaging.Subscribe(t.Context(), broker, "engine", handle, "murphy.v1.alert")
	require.NoError(t, err)
	return deliver
}

func TestPublishedMessagesContinueTheTraceInTheConsumer(t *testing.T) {
	exporter := traces(t)
	broker := newBroker(t)
	var sent []byte
	broker.EXPECT().Publish(mock.Anything, "murphy.v1.alert", "disk-full", "message-1", mock.Anything).
		RunAndReturn(func(_ context.Context, _, _, _ string, data []byte) error {
			sent = data
			return nil
		})

	ctx, request := otel.Tracer("test").Start(t.Context(), "POST /v1/alerts")
	require.NoError(t, messaging.NewPublisher(broker).Publish(ctx, "disk-full", alertReceived("message-1")))
	request.End()

	var received *murphy.Envelope
	var handlerSpan trace.SpanContext
	deliver := subscribed(t, broker, func(ctx context.Context, envelope *murphy.Envelope) error {
		received, handlerSpan = envelope, trace.SpanContextFromContext(ctx)
		return nil
	})
	require.NoError(t, deliver(context.Background(), messaging.Delivery{Data: sent, Attempt: 1}))

	require.NotNil(t, received)
	assert.Equal(t, "message-1", received.GetId())
	assert.Equal(t, "disk-full", received.GetAlert().GetCurrent().GetDedupKey())
	assert.Equal(t, request.SpanContext().TraceID(), handlerSpan.TraceID())

	spans := map[string]tracetest.SpanStub{}
	for _, span := range exporter.GetSpans() {
		spans[span.Name] = span
	}
	require.Contains(t, spans, "send murphy.v1.alert")
	require.Contains(t, spans, "process murphy.v1.alert")
	assert.Equal(t, trace.SpanKindProducer, spans["send murphy.v1.alert"].SpanKind)
	assert.Equal(t, trace.SpanKindConsumer, spans["process murphy.v1.alert"].SpanKind)
	require.Len(t, spans["process murphy.v1.alert"].Links, 1)
	assert.Equal(t, spans["send murphy.v1.alert"].SpanContext.SpanID(), spans["process murphy.v1.alert"].Links[0].SpanContext.SpanID())
}

func TestPublishRefusesIncompleteMessages(t *testing.T) {
	publisher := messaging.NewPublisher(newBroker(t))

	for name, envelope := range map[string]*murphy.Envelope{
		"no ID":    alertReceived(""),
		"no event": {Id: "message-1"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, publisher.Publish(t.Context(), "disk-full", envelope))
		})
	}
	t.Run("no key", func(t *testing.T) {
		assert.Error(t, publisher.Publish(t.Context(), "", alertReceived("message-1")))
	})
}

func TestHandlerOutcomesDecideRedelivery(t *testing.T) {
	data, err := proto.Marshal(alertReceived("message-1"))
	require.NoError(t, err)

	for name, test := range map[string]struct {
		err      error
		failed   bool
		rejected bool
	}{
		"handled":             {},
		"transient failure":   {err: errors.New("database unavailable"), failed: true},
		"invalid message":     {err: fmt.Errorf("open incident: %w", murphy.ErrInvalid), failed: true, rejected: true},
		"rejected explicitly": {err: messaging.Reject(errors.New("unknown integration")), failed: true, rejected: true},
	} {
		t.Run(name, func(t *testing.T) {
			deliver := subscribed(t, newBroker(t), func(context.Context, *murphy.Envelope) error { return test.err })

			err := deliver(t.Context(), messaging.Delivery{Data: data, Attempt: 1})

			assert.Equal(t, test.failed, err != nil)
			assert.Equal(t, test.rejected, messaging.Rejected(err))
		})
	}
}

func TestUndecodableMessagesAreRejectedWithoutReachingTheHandler(t *testing.T) {
	called := false
	deliver := subscribed(t, newBroker(t), func(context.Context, *murphy.Envelope) error {
		called = true
		return nil
	})

	err := deliver(t.Context(), messaging.Delivery{Data: []byte("not protobuf"), Attempt: 1})

	assert.True(t, messaging.Rejected(err))
	assert.False(t, called)
}

func TestSubscribeRefusesUnknownTopics(t *testing.T) {
	_, err := messaging.Subscribe(t.Context(), newBroker(t), "engine", func(context.Context, *murphy.Envelope) error { return nil }, "murphy.v1.nothing")

	assert.Error(t, err)
}

func TestRetriesBackOffUpToAMinute(t *testing.T) {
	assert.LessOrEqual(t, messaging.RetryAfter(1), time.Second)
	for attempt := 10; attempt < 50; attempt++ {
		assert.GreaterOrEqual(t, messaging.RetryAfter(attempt), 30*time.Second)
		assert.LessOrEqual(t, messaging.RetryAfter(attempt), time.Minute)
	}
}
