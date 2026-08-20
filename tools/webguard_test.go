package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// The ranges a fetch must refuse, and why each one is on the list rather than
// left to net/netip's own predicates.
func TestInternalBlocksEveryAddressAnAgentShouldNotReach(t *testing.T) {
	blocked := map[string]string{
		"127.0.0.1":              "loopback",
		"::1":                    "IPv6 loopback",
		"::ffff:127.0.0.1":       "loopback wearing an IPv6 mapping",
		"10.0.0.1":               "private",
		"172.16.0.1":             "private",
		"192.168.1.1":            "private",
		"::ffff:192.168.1.1":     "private wearing an IPv6 mapping",
		"fd00::1":                "IPv6 unique local",
		"169.254.169.254":        "the cloud metadata endpoint",
		"::ffff:169.254.169.254": "the metadata endpoint, mapped",
		"fe80::1":                "IPv6 link local",
		"0.0.0.0":                "unspecified, which means this host",
		"::":                     "unspecified, IPv6",
		"224.0.0.1":              "multicast",
		"100.64.0.1":             "carrier-grade NAT, which netip has no predicate for",
		"198.18.0.1":             "benchmarking, a real bypass in another agent's fetch tool",
		"192.0.0.1":              "IETF protocol assignments",
		"240.0.0.1":              "reserved",
		"255.255.255.255":        "broadcast",
		"64:ff9b::7f00:1":        "NAT64, a route back to IPv4",
	}

	for address, why := range blocked {
		addr := netip.MustParseAddr(address).Unmap()
		if !internal(addr) {
			t.Errorf("internal(%s) = false, want it refused — %s", address, why)
		}
	}
}

func TestInternalAllowsThePublicInternet(t *testing.T) {
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111"} {
		if internal(netip.MustParseAddr(address).Unmap()) {
			t.Errorf("internal(%s) = true, want the public internet reachable", address)
		}
	}
}

// The guard has to be in the dial, not on the URL, and this is what proves it:
// a server that really is listening, on a name that really does resolve, at an
// address the fetch must still refuse. A hostname check would pass this; a
// resolve-then-check would pass it and then race.
func TestTheGuardedClientRefusesLoopbackEvenWhenSomethingIsListening(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("secret from an internal service")); err != nil {
			t.Errorf("writing: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	page := &fetcher{client: guardedClient(DefaultFetchTimeout)}
	target, err := parseFetchURL(server.URL)
	if err != nil {
		t.Fatalf("parseFetchURL(%q): %v", server.URL, err)
	}

	body, err := page.read(context.Background(), target)
	if err == nil {
		t.Fatalf("read succeeded and returned %q, want the dial refused", body)
	}
	if !strings.Contains(err.Error(), "not on the public internet") {
		t.Errorf("error = %q, want it to say the address was refused", err)
	}
}

// localhost is the same refusal by another spelling, and worth its own case
// because it is the one a prompt-injected URL is most likely to use.
func TestTheGuardedClientRefusesLocalhostByName(t *testing.T) {
	page := &fetcher{client: guardedClient(DefaultFetchTimeout)}
	target, err := parseFetchURL("http://localhost:9/")
	if err != nil {
		t.Fatalf("parseFetchURL: %v", err)
	}

	if _, err := page.read(context.Background(), target); err == nil {
		t.Fatal("read succeeded against localhost, want it refused")
	}
}
