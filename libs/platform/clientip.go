// Package platform is the service kit every Go service of the platform shares: JSON logs, health
// endpoints, env config, bearer auth, rate limits, the client IP by proxy hop count, the base path
// and an HTTP server with graceful shutdown.
package platform

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the client address of r by architecture principle 11: it never trusts the
// leftmost X-Forwarded-For entry, which a client can forge. All X-Forwarded-For header lines are
// joined and split on commas; the client is the entry hops places from the right (hops 1 is the
// last entry, written by the ingress). With fewer entries, or one that does not parse as an IP,
// it is the TCP peer address.
func ClientIP(r *http.Request, hops int) netip.Addr {
	if hops > 0 {
		var entries []string
		for _, line := range r.Header.Values("X-Forwarded-For") {
			for _, e := range strings.Split(line, ",") {
				entries = append(entries, strings.TrimSpace(e))
			}
		}
		if len(entries) >= hops {
			if addr, err := netip.ParseAddr(entries[len(entries)-hops]); err == nil {
				return addr.Unmap()
			}
		}
	}
	return peerAddr(r)
}

func peerAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// RateKey groups addresses the way one client usually holds them: the full IPv4 address, or the
// IPv6 /64 network.
func RateKey(addr netip.Addr) string {
	if addr.Is6() {
		prefix, err := addr.Prefix(64)
		if err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}
