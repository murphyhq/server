package tracer

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/murphyhq/server/internal/telemetry"
)

type Message struct {
	System      string
	Destination string
	ID          string
	Group       string
}

func Send(ctx context.Context, m Message) (context.Context, Scope) {
	caller := telemetry.CallerOf(1)
	ctx, span := otel.Tracer(caller.Package).Start(ctx, "send "+m.Destination,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(append(m.attributes("send"), attributes(telemetry.Fields(ctx))...)...),
	)
	return ctx, Scope{span: span}
}

func Process(ctx context.Context, carrier map[string]string, m Message) (context.Context, Scope) {
	caller := telemetry.CallerOf(1)
	producer := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
	options := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(append(m.attributes("process"), attributes(telemetry.Fields(ctx))...)...),
	}
	if link := trace.SpanContextFromContext(producer); link.IsValid() {
		options = append(options, trace.WithLinks(trace.Link{SpanContext: link}))
	}
	ctx, span := otel.Tracer(caller.Package).Start(producer, "process "+m.Destination, options...)
	return ctx, Scope{span: span}
}

func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

func (m Message) attributes(operation string) []attribute.KeyValue {
	kvs := []attribute.KeyValue{
		attribute.String("messaging.system", m.System),
		attribute.String("messaging.operation.type", operation),
		attribute.String("messaging.operation.name", operation),
		attribute.String("messaging.destination.name", m.Destination),
		attribute.String("messaging.message.id", m.ID),
	}
	if m.Group != "" {
		kvs = append(kvs, attribute.String("messaging.consumer.group.name", m.Group))
	}
	return kvs
}
