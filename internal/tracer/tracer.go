package tracer

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/telemetry"
)

type Scope struct {
	span trace.Span
}

type Option func(context.Context, *[]trace.SpanStartOption) context.Context

func Producer() Option {
	return func(ctx context.Context, options *[]trace.SpanStartOption) context.Context {
		*options = append(*options, trace.WithSpanKind(trace.SpanKindProducer))
		return ctx
	}
}

func Consumer() Option {
	return func(ctx context.Context, options *[]trace.SpanStartOption) context.Context {
		*options = append(*options, trace.WithSpanKind(trace.SpanKindConsumer))
		return ctx
	}
}

func ContinuedFrom(carrier map[string]string) Option {
	return func(ctx context.Context, options *[]trace.SpanStartOption) context.Context {
		remote := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
		link := trace.SpanContextFromContext(remote)
		if !link.IsValid() {
			return ctx
		}
		*options = append(*options, trace.WithLinks(trace.Link{SpanContext: link}))
		return remote
	}
}

func Span(ctx context.Context, opts ...Option) (context.Context, Scope) {
	caller := telemetry.CallerOf(1)
	options := []trace.SpanStartOption{trace.WithAttributes(telemetry.Attributes(telemetry.Fields(ctx))...)}
	for _, opt := range opts {
		ctx = opt(ctx, &options)
	}
	ctx, span := otel.Tracer(caller.Package).Start(ctx, caller.Function, options...)
	return ctx, Scope{span: span}
}

func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

func (s Scope) Set(ctx context.Context, fields ...telemetry.Field) context.Context {
	s.span.SetAttributes(telemetry.Attributes(fields)...)
	return telemetry.With(ctx, fields...)
}

func (s Scope) End(err *error) {
	if err != nil && *err != nil {
		s.span.RecordError(*err)
		if !expected(*err) {
			s.span.SetStatus(codes.Error, (*err).Error())
		}
	}
	s.span.End()
}

func expected(err error) bool {
	return errors.Is(err, murphy.ErrInvalid) || errors.Is(err, murphy.ErrNotFound) || errors.Is(err, murphy.ErrConflict)
}

func TraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return ""
	}
	return span.TraceID().String()
}

func TraceResponse(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return ""
	}
	return fmt.Sprintf("00-%s-%s-%s", span.TraceID(), span.SpanID(), span.TraceFlags())
}
