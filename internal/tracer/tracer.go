package tracer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
)

type Scope struct {
	span trace.Span
}

func Span(ctx context.Context) (context.Context, Scope) {
	caller := telemetry.CallerOf(1)
	ctx, span := otel.Tracer(caller.Package).Start(ctx, caller.Function, trace.WithAttributes(attributes(logger.Fields(ctx))...))
	return ctx, Scope{span: span}
}

func (s Scope) Set(ctx context.Context, fields ...logger.Field) context.Context {
	s.span.SetAttributes(attributes(fields)...)
	return logger.With(ctx, fields...)
}

func (s Scope) End(err *error) {
	if err != nil && *err != nil {
		s.span.RecordError(*err)
		s.span.SetStatus(codes.Error, (*err).Error())
	}
	s.span.End()
}

func attributes(fields []logger.Field) []attribute.KeyValue {
	kvs := make([]attribute.KeyValue, 0, len(fields))
	for _, f := range fields {
		kvs = append(kvs, attributeOf(f.Name(), f.Value))
	}
	return kvs
}

func attributeOf(key string, v any) attribute.KeyValue {
	switch v := v.(type) {
	case string:
		return attribute.String(key, v)
	case bool:
		return attribute.Bool(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	case fmt.Stringer:
		return attribute.String(key, v.String())
	default:
		return attribute.String(key, fmt.Sprint(v))
	}
}
