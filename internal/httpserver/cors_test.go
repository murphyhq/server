package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/httpserver"
)

func policy(origins ...string) httpserver.CORS {
	return httpserver.CORS{
		Origins:       origins,
		Methods:       []string{"GET", "POST"},
		AllowHeaders:  []string{"Authorization", "Idempotency-Key"},
		ExposeHeaders: []string{"Idempotent-Replayed"},
	}
}

func withPolicy(t *testing.T, route http.Handler, c httpserver.CORS) http.Handler {
	t.Helper()
	server, err := httpserver.New(httpserver.Config{CORS: c}, route)
	require.NoError(t, err)
	return server.Handler()
}

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	handler := withPolicy(t, ok(), policy("http://localhost:8090"))

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	handler.ServeHTTP(allowed, req)
	assert.Equal(t, "http://localhost:8090", allowed.Header().Get("Access-Control-Allow-Origin"))

	denied := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(denied, req)
	assert.Empty(t, denied.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSExposesTraceHeadersAndTheDeclaredOnes(t *testing.T) {
	handler := withPolicy(t, ok(), policy("http://localhost:8090"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "X-Trace-Id, traceresponse, Idempotent-Replayed", rec.Header().Get("Access-Control-Expose-Headers"))
}

func TestCORSAnswersPreflightWithTheDeclaredPolicy(t *testing.T) {
	reached := false
	route := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	handler := withPolicy(t, route, policy("http://localhost:8090"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "GET, POST", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Authorization, Idempotency-Key", rec.Header().Get("Access-Control-Allow-Headers"))
	assert.False(t, reached)
}

func TestCORSSendsNothingWithoutConfiguredOrigins(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	withPolicy(t, ok(), policy()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestNewRejectsOnlyBareOrigins(t *testing.T) {
	for _, valid := range [][]string{nil, {"http://localhost:8090", "https://app.example.com:8443"}} {
		_, err := httpserver.New(httpserver.Config{CORS: policy(valid...)}, ok())
		require.NoError(t, err)
	}

	for _, bad := range []string{"*", "example.com", "ftp://example.com", "https://app.example.com/", "https://app.example.com/path", "https://user@app.example.com"} {
		_, err := httpserver.New(httpserver.Config{CORS: policy(bad)}, ok())
		assert.Error(t, err, bad)
	}
}

func TestNewErrorsNeverEchoTheValue(t *testing.T) {
	_, err := httpserver.New(httpserver.Config{CORS: policy("https://app.example.com", "s3cret-host")}, ok())
	require.Error(t, err)
	assert.ErrorContains(t, err, "entry 2")
	assert.NotContains(t, err.Error(), "s3cret-host")
}
