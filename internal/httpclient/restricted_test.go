package httpclient_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/httpclient"
)

func TestInternalAddressesAreRefused(t *testing.T) {
	guard := httpclient.Guard(nil)
	internal := []string{
		"127.0.0.1:80", "[::1]:80", "0.0.0.0:80", "[::]:80",
		"10.1.2.3:443", "172.16.0.1:443", "192.168.1.1:80", "[fc00::1]:443",
		"169.254.169.254:80", "[fe80::1]:443", "100.64.0.1:80", "224.0.0.1:80",
		"[::ffff:127.0.0.1]:80", "[::ffff:169.254.169.254]:80",
	}

	for _, address := range internal {
		assert.ErrorIs(t, guard("tcp", address, nil), httpclient.ErrForbiddenAddress, address)
	}
}

func TestPublicAddressesAreAllowed(t *testing.T) {
	guard := httpclient.Guard(nil)

	assert.NoError(t, guard("tcp", "93.184.216.34:443", nil))
	assert.NoError(t, guard("tcp", "[2606:4700::1111]:443", nil))
}

func TestAnAllowedNetworkIsReachable(t *testing.T) {
	guard := httpclient.Guard([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})

	assert.NoError(t, guard("tcp", "10.1.2.3:443", nil))
	assert.ErrorIs(t, guard("tcp", "192.168.1.1:443", nil), httpclient.ErrForbiddenAddress)
}

func TestARestrictedClientRefusesToConnectToAnInternalAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	refusing := httpclient.Restricted(time.Second, nil)
	trusting := httpclient.Restricted(time.Second, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})

	_, refused := refusing.Get(server.URL)
	allowed, err := trusting.Get(server.URL)

	assert.ErrorIs(t, refused, httpclient.ErrForbiddenAddress)
	require.NoError(t, err)
	_ = allowed.Body.Close()
	assert.Equal(t, http.StatusNoContent, allowed.StatusCode)
}
