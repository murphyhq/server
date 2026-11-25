package telemetry

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const prefix = "murphy."

type Field struct {
	Key      string
	Value    any
	standard bool
}

func (f Field) Name() string {
	if f.standard || strings.HasPrefix(f.Key, prefix) {
		return f.Key
	}
	return prefix + f.Key
}

func Standard(key string, value any) Field {
	return Field{Key: key, Value: value, standard: true}
}

func WithField(key string, value any) Field {
	return Field{Key: key, Value: value}
}

func WithError(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: ""}
	}
	return Field{Key: "error", Value: err.Error()}
}

type contextKey struct{}

func With(ctx context.Context, fields ...Field) context.Context {
	if len(fields) == 0 {
		return ctx
	}
	existing := Fields(ctx)
	combined := make([]Field, 0, len(existing)+len(fields))
	for _, f := range existing {
		if !replaced(f, fields) {
			combined = append(combined, f)
		}
	}
	combined = append(combined, fields...)
	return context.WithValue(ctx, contextKey{}, combined)
}

func replaced(f Field, fields []Field) bool {
	for _, n := range fields {
		if n.Name() == f.Name() {
			return true
		}
	}
	return false
}

func Fields(ctx context.Context) []Field {
	fields, _ := ctx.Value(contextKey{}).([]Field)
	return fields
}

type componentKey struct{}

func WithComponent(ctx context.Context, name string) context.Context {
	return context.WithValue(With(ctx, WithField("component", name)), componentKey{}, name)
}

func Component(ctx context.Context) string {
	name, _ := ctx.Value(componentKey{}).(string)
	return name
}

func ComponentAttributes(ctx context.Context) []attribute.KeyValue {
	if name := Component(ctx); name != "" {
		return []attribute.KeyValue{attribute.String(prefix+"component", name)}
	}
	return nil
}

type collectedKey struct{}

type collected struct {
	mu     sync.Mutex
	fields []Field
}

func Collecting(ctx context.Context) context.Context {
	return context.WithValue(ctx, collectedKey{}, &collected{})
}

func Collect(ctx context.Context, fields ...Field) {
	c, ok := ctx.Value(collectedKey{}).(*collected)
	if !ok {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(Attributes(fields)...)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, field := range fields {
		c.fields = slices.DeleteFunc(c.fields, func(collected Field) bool { return collected.Name() == field.Name() })
		c.fields = append(c.fields, field)
	}
}

func Collected(ctx context.Context) []Field {
	c, ok := ctx.Value(collectedKey{}).(*collected)
	if !ok {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.fields)
}

func Attributes(fields []Field) []attribute.KeyValue {
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
