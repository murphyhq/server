package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/murphyhq/server/internal/httpserver"
)

var proxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

func addressOf(remote, forwardedFor string) string {
	var seen string
	handler := httpserver.WithClientAddress(proxies)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = httpserver.ClientAddress(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = remote
	request.Header.Set("X-Forwarded-For", forwardedFor)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	return seen
}

func TestAClientConnectingDirectlyIsItsOwnAddress(t *testing.T) {
	assert.Equal(t, "203.0.113.7", addressOf("203.0.113.7:5000", ""))
}

func TestAnUntrustedPeerCantClaimAnotherAddress(t *testing.T) {
	assert.Equal(t, "203.0.113.7", addressOf("203.0.113.7:5000", "198.51.100.1"))
}

func TestATrustedProxyReportsTheClientsAddress(t *testing.T) {
	assert.Equal(t, "198.51.100.1", addressOf("10.0.0.2:5000", "198.51.100.1"))
}

func TestTheNearestUntrustedHopIsTheClient(t *testing.T) {
	assert.Equal(t, "198.51.100.1", addressOf("10.0.0.2:5000", "192.0.2.9, 198.51.100.1, 10.0.0.3"), "what the client wrote itself isn't believed")
}

func TestAProxyWithoutAHeaderIsTheAddress(t *testing.T) {
	assert.Equal(t, "10.0.0.2", addressOf("10.0.0.2:5000", ""))
}
