package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Remote is one MCP server reached over HTTP, using the Streamable HTTP
// transport the specification defines for a client that dials out.
//
// # How this differs from mcp.Server
//
// They describe the same kind of server and differ in who dials it, which
// changes more than it sounds like. An mcp.Server is handed to the Claude
// API, which connects to it from its own side: the tools run there, this
// process never speaks the protocol, and the whole arrangement exists only
// on the backend that offers a connector — openrouter refuses a config
// carrying one under the capability rule.
//
// A Remote is dialled here. The tools arrive as ordinary nacelle.Tool
// values, so they pass through Config.Approve, emit the same events as every
// other tool, and work on both backends. The cost is that the traffic is
// this machine's: a server the Claude API could have reached on its own is
// now reached over your egress, once per call.
//
// Prefer this one from a client that lets a person switch backends, because
// a tool set that changes shape with -backend is a worse surprise than an
// extra round trip. Prefer mcp.Server from a service pinned to Anthropic
// that would rather not carry the traffic.
type Remote struct {
	// Name namespaces this server's tools and must be unique in one call
	// to Connect. Every tool is presented to the model as <Name>_<tool>,
	// always — a tool's name must not change depending on how many servers
	// happen to be configured.
	Name string

	// URL is the endpoint. Required, absolute, and http or https.
	URL string

	// Headers are sent with every request, which is where a bearer token
	// goes: Headers["Authorization"] = "Bearer " + token.
	//
	// Read from configuration rather than written into one. A token in a
	// struct literal is a token in git, which is the same rule mcp.Server
	// states for the credential it carries.
	Headers map[string]string

	// AllowedTools restricts what the model may call on this server. Empty
	// allows every tool the server exposes.
	//
	// Worth setting for any server that can write, and worth more here
	// than on a Command: a subprocess is a program you installed, and this
	// is a URL whose tool list can change without anyone here redeploying.
	//
	// It is set here, in Go. The mcpServers format Load reads has no key for
	// an allow-list, so a server configured from a .mcp.json gets every tool
	// it offers — worth knowing before taking the advice above, because that
	// file is how most callers will configure one. Narrowing today means
	// building the value by hand instead of loading it.
	AllowedTools []string

	// Timeout bounds one tools/call and the connect-time handshake.
	// Defaults to DefaultCallTimeout.
	Timeout time.Duration

	// Token mints the bearer token for each request, from the context of the
	// call that sends it, so one connection can carry a different user's
	// delegated token on every call. Connect calls it with its own context.
	// A 401 calls it once more, with [Refreshing] true, and replays the
	// request; a second 401 is the caller's answer. Setting both Token and an
	// Authorization header is refused.
	Token TokenSource

	// Identity names who the token for ctx belongs to. A tools/list result the
	// server marks cacheScope "private" is cached under it, and never shared
	// with another identity. With a Token and no Identity, private results are
	// not cached at all, because there is nothing safe to key them by.
	Identity func(ctx context.Context) string

	// PrivateHosts are host names this server may reach at a private address,
	// such as a first-party service on an internal network. The SSRF guard
	// refuses every private address by default, and nothing in the mcpServers
	// format can set this, so an internal target is always a choice made in Go
	// for one server. Link-local addresses stay refused whatever is listed.
	PrivateHosts []string

	// Elicit answers a server that needs a person's input before it finishes
	// a call, such as a confirmation before sending mail. Without it this
	// client does not advertise elicitation, and a well-behaved server takes
	// its no-confirmation path instead.
	Elicit Elicit
}

func (r Remote) details() details {
	return details{
		name: r.Name, allowed: r.AllowedTools, timeout: timeoutOr(r.Timeout),
		elicit: r.Elicit, identity: r.identity(),
	}
}

// identity is the key private tool lists are cached under. A server with no
// per-call token has one principal, so the empty identity is safe for it; a
// server with one and no Identity gets none, which turns private caching off.
func (r Remote) identity() func(context.Context) string {
	if r.Token == nil || r.Identity != nil {
		return r.Identity
	}
	return func(context.Context) string { return uncacheable }
}

// check refuses an endpoint that cannot be dialled.
//
// The scheme is checked rather than left to fail at connect time because the
// failure it prevents is unreadable: MCP also defines an SSE transport and
// clients in this ecosystem accept ws:// URLs, so a config written for one
// of those arrives here as a URL that looks fine and a session that never
// establishes. Naming the scheme says which line of the file to change.
//
// The host is resolved and the target addresses are checked against private
// and link-local ranges, which catches cloud-metadata SSRF and internal-
// network confusion at configuration time rather than at the moment a tool
// first runs. DNS resolution is checked again at dial time through the same
// transport wrapper (see secured), so a DNS rebinding attack between the two
// points is still caught.
func (r Remote) check() error {
	if r.URL == "" {
		return fmt.Errorf("nacelle/mcp/client: server %q has no URL", r.Name)
	}
	parsed, err := url.Parse(r.URL)
	if err != nil {
		return fmt.Errorf("nacelle/mcp/client: server %q has an unreadable URL: %w", r.Name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf(
			"nacelle/mcp/client: server %q speaks %q, and this client speaks http and https — "+
				"the Streamable HTTP transport is the one MCP defines for a client that dials out",
			r.Name, parsed.Scheme)
	}
	if r.Token != nil && hasAuthorization(r.Headers) {
		return fmt.Errorf("nacelle/mcp/client: server %q sets both Token and an Authorization header; keep one", r.Name)
	}
	return checkTarget(parsed.Host, r.Name, r.PrivateHosts)
}

// hasAuthorization reports whether headers carry an Authorization header in
// any spelling, since HTTP header names are case-insensitive.
func hasAuthorization(headers map[string]string) bool {
	for key := range headers {
		if strings.EqualFold(key, "Authorization") {
			return true
		}
	}
	return false
}

// checkTarget resolves a hostname and rejects private and link-local addresses.
//
// Loopback is allowed: local development servers and Docker-mapped ports are
// the most common targets. Cloud metadata (169.254.x.x) and the RFC 1918 and
// ULA private ranges are refused as SSRF defence, unless the host is one the
// caller listed in private. Link-local is refused even then, because no
// first-party MCP server lives at a metadata address.
//
// The host may be a bare IP, a name, or name:port.
func checkTarget(hostport, name string, private []string) error {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	trusted := slices.ContainsFunc(private, func(listed string) bool { return strings.EqualFold(listed, host) })

	ips, _ := net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
	for _, ip := range ips {
		switch {
		case ip.IsLoopback():
		case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
			return fmt.Errorf(
				"nacelle/mcp/client: server %q at %q resolves to %s, which is a link-local address — "+
					"SSRF risk to cloud metadata endpoints", name, host, ip)
		case ip.IsPrivate() && !trusted:
			return fmt.Errorf(
				"nacelle/mcp/client: server %q at %q resolves to %s, which is a private address — "+
					"SSRF risk; list the host in PrivateHosts if it is a first-party internal server", name, host, ip)
		}
	}
	return nil
}

// dial builds the Streamable HTTP transport over the rate-limit retry, the
// SSRF guard, the configured headers and the per-call token, in that order
// inward, so a request sent again is checked and authorised again.
func (r Remote) dial() (sdk.Transport, *diagnostics, error) {
	var chain http.RoundTripper = authorized{base: http.DefaultTransport, token: r.Token, server: r.Name}
	chain = headed{base: chain, headers: r.Headers}
	chain = secured{base: chain, server: r.Name, private: r.PrivateHosts}
	chain = throttled{base: chain, server: r.Name}
	transport := &sdk.StreamableClientTransport{
		Endpoint: r.URL, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: chain},
	}
	return transport, &diagnostics{}, nil
}
