package logger

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/murphyhq/server/internal/telemetry"
)

const prefix = "murphy."

type Field struct {
	Key   string
	Value any
}

func (f Field) Name() string {
	if strings.HasPrefix(f.Key, prefix) {
		return f.Key
	}
	return prefix + f.Key
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

func Debug(ctx context.Context, msg string, fields ...Field) {
	write(ctx, slog.LevelDebug, msg, fields)
}

func Info(ctx context.Context, msg string, fields ...Field) {
	write(ctx, slog.LevelInfo, msg, fields)
}

func Warn(ctx context.Context, msg string, fields ...Field) {
	write(ctx, slog.LevelWarn, msg, fields)
}

func Error(ctx context.Context, msg string, fields ...Field) {
	write(ctx, slog.LevelError, msg, fields)
}

func write(ctx context.Context, level slog.Level, msg string, fields []Field) {
	inherited := Fields(ctx)
	attrs := make([]slog.Attr, 0, len(inherited)+len(fields))
	for _, f := range inherited {
		attrs = append(attrs, slog.Any(f.Name(), value(f.Value)))
	}
	for _, f := range fields {
		attrs = append(attrs, slog.Any(f.Name(), value(f.Value)))
	}
	telemetry.Logger(telemetry.CallerOf(2).Package).LogAttrs(ctx, level, msg, attrs...)
}

func value(v any) any {
	switch v := v.(type) {
	case string, bool, int, int64, float64:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}
