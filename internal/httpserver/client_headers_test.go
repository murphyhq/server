package httpserver_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	"github.com/murphyhq/server/internal/httpserver"
)

func requestWithAppHeaders(t *testing.T, headers map[string]string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /incidents", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/incidents", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	httpserver.Handler(mux).ServeHTTP(httptest.NewRecorder(), request)
}

func attributeValues(attributes []attribute.KeyValue) map[string]string {
	values := map[string]string{}
	for _, kv := range attributes {
		values[string(kv.Key)] = kv.Value.Emit()
	}
	return values
}

func TestAnAppsDeviceIsOnItsRequestsSpanAndLog(t *testing.T) {
	exporter := traced(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	requestWithAppHeaders(t, map[string]string{
		"Murphy-App-Version":  "1.4.2",
		"Murphy-App-Build":    "57",
		"Murphy-OS":           "iOS",
		"Murphy-OS-Version":   "26.1",
		"Murphy-Device-Model": "iPhone 16",
		"Murphy-Locale":       "pt-BR",
	})

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans)
	values := attributeValues(spans[len(spans)-1].Attributes)
	assert.Equal(t, "1.4.2", values["murphy.client.app.version"])
	assert.Equal(t, "57", values["murphy.client.app.build"])
	assert.Equal(t, "iOS", values["os.name"])
	assert.Equal(t, "26.1", values["os.version"])
	assert.Equal(t, "iPhone 16", values["device.model.name"])
	assert.Equal(t, "pt-BR", values["murphy.client.locale"])

	var line map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &line))
	assert.Equal(t, "iOS", line["os.name"])
	assert.Equal(t, "iPhone 16", line["device.model.name"])
}

func TestAClientsValuesAreCleanedAndCapped(t *testing.T) {
	exporter := traced(t)

	requestWithAppHeaders(t, map[string]string{
		"Murphy-Device-Model": "iPhone\x00 16 " + strings.Repeat("x", 200),
	})

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans)
	model := attributeValues(spans[len(spans)-1].Attributes)["device.model.name"]
	assert.Len(t, []rune(model), 64)
	assert.NotContains(t, model, "\x00")
	assert.True(t, strings.HasPrefix(model, "iPhone 16 "))
}
