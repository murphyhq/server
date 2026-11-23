package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/httpserver"
)

func cors(origins ...string) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return httpserver.CORS(origins)(ok)
}

func TestCORSAllowsOnlyConfiguredOrigins(t *testing.T) {
	handler := cors("http://localhost:8090")

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
	handler := httpserver.CORS([]string{"http://localhost:8090"})(route)

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
	cors().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestValidateOriginsAcceptsOnlyBareOrigins(t *testing.T) {
	require.NoError(t, httpserver.ValidateOrigins(nil))
	require.NoError(t, httpserver.ValidateOrigins([]string{"http://localhost:8090", "https://app.example.com:8443"}))

	for _, bad := range []string{"*", "example.com", "ftp://example.com", "https://app.example.com/", "https://app.example.com/path", "https://user@app.example.com"} {
		assert.Error(t, httpserver.ValidateOrigins([]string{bad}), bad)
	}
}

func TestValidateOriginsErrorsNeverEchoTheValue(t *testing.T) {
	err := httpserver.ValidateOrigins([]string{"https://app.example.com", "s3cret-host"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "entry 2")
	assert.NotContains(t, err.Error(), "s3cret-host")
}
