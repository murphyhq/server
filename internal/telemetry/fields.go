package telemetry

import (
	"context"
	"strings"
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
