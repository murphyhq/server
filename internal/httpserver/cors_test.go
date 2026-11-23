package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/httpserver"
)

func cors(t *testing.T, origins ...string) http.Handler {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	middleware, err := httpserver.CORS(origins)
	require.NoError(t, err)
	return middleware(ok)
}

func corsErr(origins ...string) error {
	_, err := httpserver.CORS(origins)
	return err
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	handler := cors(t, "http://localhost:8090")

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	handler.ServeHTTP(allowed, req)
	assert.Equal(t, "http://localhost:8090", allowed.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, allowed.Header().Get("Access-Control-Expose-Headers"), "X-Trace-Id")

	denied := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(denied, req)
	assert.Empty(t, denied.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSAnswersPreflightWithoutReachingTheRoute(t *testing.T) {
	reached := false
	route := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	middleware, err := httpserver.CORS([]string{"http://localhost:8090"})
	require.NoError(t, err)
	handler := middleware(route)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key")
	assert.False(t, reached)
}

func TestCORSSendsNothingWithoutConfiguredOrigins(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/services", nil)
	req.Header.Set("Origin", "http://localhost:8090")
	cors(t).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSAcceptsOnlyBareOrigins(t *testing.T) {
	require.NoError(t, corsErr())
	require.NoError(t, corsErr("http://localhost:8090", "https://app.example.com:8443"))

	for _, bad := range []string{"*", "example.com", "ftp://example.com", "https://app.example.com/", "https://app.example.com/path", "https://user@app.example.com"} {
		assert.Error(t, corsErr(bad), bad)
	}
}

func TestCORSErrorsNeverEchoTheValue(t *testing.T) {
	err := corsErr("https://app.example.com", "s3cret-host")
	require.Error(t, err)
	assert.ErrorContains(t, err, "entry 2")
	assert.NotContains(t, err.Error(), "s3cret-host")
}
