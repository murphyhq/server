package logger

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/murphyhq/server/internal/telemetry"
)

func Debug(ctx context.Context, msg string, fields ...telemetry.Field) {
	write(ctx, slog.LevelDebug, msg, fields)
}

func Info(ctx context.Context, msg string, fields ...telemetry.Field) {
	write(ctx, slog.LevelInfo, msg, fields)
}

func Warn(ctx context.Context, msg string, fields ...telemetry.Field) {
	write(ctx, slog.LevelWarn, msg, fields)
}

func Error(ctx context.Context, msg string, fields ...telemetry.Field) {
	write(ctx, slog.LevelError, msg, fields)
}

func write(ctx context.Context, level slog.Level, msg string, fields []telemetry.Field) {
	inherited := telemetry.Fields(ctx)
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
