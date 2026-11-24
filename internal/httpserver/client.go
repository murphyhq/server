package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type clientAddressKey struct{}

func WithClientAddress(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), clientAddressKey{}, clientAddress(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ClientAddress(ctx context.Context) string {
	address, _ := ctx.Value(clientAddressKey{}).(string)
	return address
}

func clientAddress(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	isTrusted := func(address netip.Addr) bool {
		return slices.ContainsFunc(trusted, func(network netip.Prefix) bool { return network.Contains(address.Unmap()) })
	}
	if !isTrusted(peer) {
		return peer.Unmap().String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !isTrusted(hop) {
			return hop.Unmap().String()
		}
	}
	return peer.Unmap().String()
}
