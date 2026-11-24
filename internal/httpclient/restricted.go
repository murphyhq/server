package httpclient

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"syscall"
	"time"
)

var ErrForbiddenAddress = errors.New("the address is on a network this client may not reach")

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func Restricted(timeout time.Duration, allowed []netip.Prefix) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: Guard(allowed)}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{Timeout: timeout, Transport: Instrument(transport)}
}

func Guard(allowed []netip.Prefix) func(network, address string, conn syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		target, err := netip.ParseAddrPort(address)
		if err != nil {
			return ErrForbiddenAddress
		}
		ip := target.Addr().Unmap()
		if public(ip) || slices.ContainsFunc(allowed, func(network netip.Prefix) bool { return network.Contains(ip) }) {
			return nil
		}
		return ErrForbiddenAddress
	}
}

func public(ip netip.Addr) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !sharedAddressSpace.Contains(ip)
}
