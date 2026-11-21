package httpserver_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpserver"
)

func serve(t *testing.T) (*httptest.Server, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagator)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "broken" {
			httpserver.WriteError(w, r, errors.New("database unavailable"))
			return
		}
		httpserver.WriteError(w, r, &murphy.NotFoundError{Entity: "thing"})
	})
	server := httptest.NewServer(httpserver.Handler(mux))
	t.Cleanup(server.Close)
	return server, exporter
}

func get(t *testing.T, url string, header http.Header) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	for key, values := range header {
		req.Header[key] = values
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return resp, body
}

func span(t *testing.T, exporter *tracetest.InMemoryExporter) tracetest.SpanStub {
	t.Helper()
	require.Eventually(t, func() bool { return len(exporter.GetSpans()) == 1 }, time.Second, 10*time.Millisecond)
	return exporter.GetSpans()[0]
}

func TestRequestsAreTracedAndAnswerWithTheirTraceID(t *testing.T) {
	server, exporter := serve(t)

	resp, body := get(t, server.URL+"/things/42", nil)

	recorded := span(t, exporter)
	traceID := recorded.SpanContext.TraceID().String()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, traceID, resp.Header.Get("X-Trace-Id"))
	assert.Contains(t, resp.Header.Get("traceresponse"), traceID)
	assert.Equal(t, traceID, body["trace_id"])
	assert.Equal(t, "GET /things/{id}", recorded.Name)
	assert.Contains(t, recorded.Attributes, attributeOf("http.route", "/things/{id}"))
	assert.Equal(t, codes.Unset, recorded.Status.Code, "a 4xx is not a server failure")
}

func TestRequestsContinueTheCallersTrace(t *testing.T) {
	server, exporter := serve(t)
	caller := "4bf92f3577b34da6a3ce929d0e0e4736"

	resp, _ := get(t, server.URL+"/things/42", http.Header{"Traceparent": {"00-" + caller + "-00f067aa0ba902b7-01"}})

	assert.Equal(t, caller, span(t, exporter).SpanContext.TraceID().String())
	assert.Equal(t, caller, resp.Header.Get("X-Trace-Id"))
}

func TestServerFailuresFailTheSpan(t *testing.T) {
	server, exporter := serve(t)

	resp, body := get(t, server.URL+"/things/broken", nil)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.NotContains(t, body, "detail")
	assert.Equal(t, codes.Error, span(t, exporter).Status.Code)
}

func TestUnmatchedRoutesAnswerWithProblems(t *testing.T) {
	server, exporter := serve(t)

	resp, body := get(t, server.URL+"/nothing-here", nil)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))
	assert.Equal(t, resp.Header.Get("X-Trace-Id"), body["trace_id"])
	assert.Equal(t, "GET", span(t, exporter).Name)
}

func attributeOf(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}
