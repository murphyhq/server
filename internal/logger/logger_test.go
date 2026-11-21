package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

func capture(t *testing.T) func() []map[string]any {
	t.Helper()
	lines, _ := captureRaw(t, "debug")
	return lines
}

func captureRaw(t *testing.T, level string) (func() []map[string]any, *bytes.Buffer) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	var out bytes.Buffer
	flush, err := telemetry.Setup(context.Background(), "test", level, &out)
	require.NoError(t, err)
	t.Cleanup(func() { _ = flush(context.Background()) })

	return func() []map[string]any {
		var lines []map[string]any
		for _, raw := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
			var line map[string]any
			require.NoError(t, json.Unmarshal(raw, &line), "log line is not JSON: %s", raw)
			lines = append(lines, line)
		}
		return lines
	}, &out
}

func TestFieldsArePrefixedOnce(t *testing.T) {
	lines := capture(t)

	logger.Info(context.Background(), "paging", logger.WithField("incident.id", "inc-1"), logger.WithField("murphy.step", 2))

	got := lines()[0]
	assert.Equal(t, "inc-1", got["murphy.incident.id"])
	assert.Equal(t, float64(2), got["murphy.step"])
}

func TestContextFieldsAppearOnEveryLogLine(t *testing.T) {
	lines := capture(t)

	ctx := logger.With(context.Background(), logger.WithField("incident.id", "inc-1"))
	logger.Info(ctx, "paging")
	logger.Error(ctx, "page failed", logger.WithError(errors.New("provider timeout")))
	logger.Info(context.Background(), "idle")

	got := lines()
	require.Len(t, got, 3)
	assert.Equal(t, "inc-1", got[0]["murphy.incident.id"])
	assert.Equal(t, "inc-1", got[1]["murphy.incident.id"])
	assert.Equal(t, "provider timeout", got[1]["murphy.error"])
	assert.Equal(t, "ERROR", got[1]["level"])
	assert.NotContains(t, got[2], "murphy.incident.id")
}

func TestLogLinesInsideASpanCarryTraceIDs(t *testing.T) {
	lines := capture(t)

	ctx, span := tracer.Span(context.Background())
	logger.Info(ctx, "paging")
	span.End(nil)
	logger.Info(context.Background(), "idle")

	got := lines()
	require.Len(t, got, 2)
	assert.Contains(t, got[0], "trace_id")
	assert.Contains(t, got[0], "span_id")
	assert.NotContains(t, got[1], "trace_id")
}

func TestRepeatedFieldsReplaceEachOther(t *testing.T) {
	lines, raw := captureRaw(t, "debug")

	ctx := logger.With(context.Background(), logger.WithField("incident.id", "inc-1"))
	ctx = logger.With(ctx, logger.WithField("incident.id", "inc-2"))
	logger.Info(ctx, "paging")

	assert.Equal(t, "inc-2", lines()[0]["murphy.incident.id"])
	assert.Equal(t, 1, bytes.Count(raw.Bytes(), []byte(`"murphy.incident.id"`)))
}

func TestLogLevelFiltersLowerLevels(t *testing.T) {
	lines, _ := captureRaw(t, "warn")

	logger.Info(context.Background(), "routine")
	logger.Warn(context.Background(), "provider slow")

	got := lines()
	require.Len(t, got, 1)
	assert.Equal(t, "provider slow", got[0]["msg"])
}
