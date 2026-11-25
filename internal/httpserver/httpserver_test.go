package httpserver_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
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
	"github.com/murphyhq/server/internal/telemetry"
)

func traced(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	previous, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return exporter
}

func serve(t *testing.T) (*httptest.Server, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := traced(t)

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

func TestTheRequestLogSaysWhoAskedAndWhyTheyWereRefused(t *testing.T) {
	exporter := traced(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	mux := http.NewServeMux()
	mux.HandleFunc("POST /incidents/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		telemetry.Collect(r.Context(), telemetry.WithField("user.id", "bo"))
		httpserver.WriteError(w, r, &murphy.ForbiddenError{Reason: "only responders can resolve this"})
	})

	httpserver.Handler(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/incidents/1/resolve", nil))

	var line map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &line))
	assert.Equal(t, "request handled", line["msg"])
	assert.Equal(t, "bo", line["murphy.user.id"])
	assert.Equal(t, "only responders can resolve this", line["murphy.refusal"])
	assert.Contains(t, span(t, exporter).Attributes, attributeOf("murphy.user.id", "bo"))
}

func attributeOf(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

func corsPolicy(origins ...string) httpserver.CORS {
	return httpserver.CORS{
		Origins:       origins,
		Methods:       []string{"GET", "POST"},
		AllowHeaders:  []string{"Authorization", "Idempotency-Key"},
		ExposeHeaders: []string{"Idempotent-Replayed"},
	}
}

func corsServer(c httpserver.CORS, route http.Handler) http.Handler {
	return httpserver.New(httpserver.Config{CORS: c}, route).Handler()
}

func TestCORSAllowsDeclaredOriginsAndAnswersPreflightsWithTheDeclaredPolicy(t *testing.T) {
	reached := false
	route := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	handler := corsServer(corsPolicy("http://localhost:8090"), route)

	get := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	handler.ServeHTTP(get, req)
	assert.Equal(t, "http://localhost:8090", get.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "X-Trace-Id, traceresponse, Idempotent-Replayed", get.Header().Get("Access-Control-Expose-Headers"))

	preflight := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodOptions, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	reached = false
	handler.ServeHTTP(preflight, req)
	assert.Equal(t, http.StatusNoContent, preflight.Code)
	assert.Equal(t, "GET, POST", preflight.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Authorization, Idempotency-Key", preflight.Header().Get("Access-Control-Allow-Headers"))
	assert.False(t, reached)
}

func TestCORSSendsNoOriginToUndeclaredCallers(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	for _, c := range []httpserver.CORS{corsPolicy("http://localhost:8090"), corsPolicy()} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
		req.Header.Set("Origin", "https://evil.example")
		corsServer(c, ok).ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSLetsDeclaredOriginsSendCredentialsOnlyWhenAllowed(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	withCredentials := corsPolicy("http://localhost:8090")
	withCredentials.Credentials = true

	declared, undeclared, without := httptest.NewRecorder(), httptest.NewRecorder(), httptest.NewRecorder()
	corsServer(withCredentials, ok).ServeHTTP(declared, requestFrom("http://localhost:8090"))
	corsServer(withCredentials, ok).ServeHTTP(undeclared, requestFrom("https://evil.example"))
	corsServer(corsPolicy("http://localhost:8090"), ok).ServeHTTP(without, requestFrom("http://localhost:8090"))

	assert.Equal(t, "true", declared.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, undeclared.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, without.Header().Get("Access-Control-Allow-Credentials"))
}

func requestFrom(origin string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", origin)
	return req
}
