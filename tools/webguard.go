package tools

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// reserved are the ranges net/netip has no predicate for, and that a fetch
// must still refuse.
//
// The helpers cover loopback, private, link-local and multicast, and they
// cover them through an IPv4-mapped IPv6 address too — ::ffff:127.0.0.1 reads
// as loopback, verified rather than assumed. What they do not cover is the
// rest of the special-use registry, and that gap is not theoretical:
// 198.18.0.0/15 was a real SSRF bypass in another agent's fetch tool, because
// it is reachable, it is not private by any predicate, and it is routed
// somewhere surprising on a benchmarking network.
//
// In order: carrier-grade NAT, IETF protocol assignments, documentation,
// benchmarking, two more documentation ranges, everything reserved from 240
// up including the broadcast address, NAT64 as a route back to IPv4, and the
// IPv6 documentation range.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// guardedClient is an HTTP client that cannot reach the machine it runs on, or
// the network that machine sits in.
//
// The check lives in the dialer's Control hook, which is the only place it can
// be correct. Checking the hostname is useless — a name resolves to whatever
// its owner says today. Resolving the name and checking the answer is a race:
// the address is looked up again when the connection is made, so a DNS server
// answering differently the second time walks straight through, which is the
// whole of DNS rebinding. Control runs after resolution and before connect,
// on the address actually about to be dialled, and it runs again for every
// redirect hop, so neither hole is open.
//
// This is the same rule as Config.Root one layer down. A model naming the host
// a request goes to is a model choosing what the process may reach, and every
// internal service sharing an egress path with the agent — a metadata endpoint
// at 169.254.169.254, an unauthenticated admin port on localhost, a database
// on the private network — is otherwise one prompt-injected URL away.
func guardedClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: blockInternal}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxFetchRedirects {
				return fmt.Errorf("stopped after %d redirects", maxFetchRedirects)
			}
			return nil
		},
	}
}

// blockInternal refuses a connection to an address the model should not be
// able to reach. It is the dialer's Control hook, so returning an error here
// fails the dial rather than filtering a result afterwards.
func blockInternal(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("refusing an address that cannot be parsed: %s", address)
	}
	if addr := addrPort.Addr().Unmap(); internal(addr) {
		return fmt.Errorf("refusing to connect to %s, which is not on the public internet", addr)
	}
	return nil
}

// internal reports whether an address belongs to this machine, this network,
// or a range that has no business answering a web fetch.
func internal(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true // invalid addresses are treated as unsafe
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() {
		return true
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
