package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
}

func (r Remote) details() details {
	return details{name: r.Name, allowed: r.AllowedTools, timeout: timeoutOr(r.Timeout)}
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
	if err := checkTarget(parsed.Host, r.Name); err != nil {
		return err
	}
	return nil
}

// checkTarget resolves a hostname and rejects private and link-local addresses.
//
// Loopback is allowed — local development servers and Docker-mapped ports are
// the most common MCP Remote targets — but cloud metadata (169.254.x.x) and
// RFC 1918 private ranges are blocked as SSRF defence. Callers running an MCP
// server on a private network can set it up over HTTPS with a DNS name instead
// of a bare IP, or use the stdio transport for a fully local setup.
//
// The host may be a bare IP, a name, or name:port. resolve strips the port.
func checkTarget(hostport, name string) error {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}

	ips, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
	if err != nil {
		return nil
	}

	for _, ip := range ips {
		if ip.IsLoopback() {
			continue
		}
		if ip.IsPrivate() {
			return fmt.Errorf(
				"nacelle/mcp/client: server %q at %q resolves to %s, which is a private address — "+
					"SSRF risk; use a DNS name or the stdio transport", name, host, ip)
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf(
				"nacelle/mcp/client: server %q at %q resolves to %s, which is a link-local address — "+
					"SSRF risk to cloud metadata endpoints", name, host, ip)
		}
	}
	return nil
}

// dial builds the Streamable HTTP transport, with SSRF-hardened transport.
func (r Remote) dial() (sdk.Transport, *diagnostics, error) {
	transport := &sdk.StreamableClientTransport{Endpoint: r.URL, DisableStandaloneSSE: true}
	wrap := &secured{}
	if len(r.Headers) > 0 {
		wrap.headers = r.Headers
	}
	transport.HTTPClient = &http.Client{Transport: wrap}
	return transport, &diagnostics{}, nil
}

// secured is an [http.RoundTripper] that SSRF-hardens every connection by
// validating the target IP on every dial, catching DNS rebinding and TOCTOU
// between the check-time resolution in checkTarget and the actual connect.
//
// It also injects the server's custom headers, through the same headed
// mechanism — a RoundTripper sees every request, including retries the
// transport handles internally.
type secured struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip validates the target IP and injects custom headers.
func (s *secured) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL != nil && request.URL.Host != "" {
		if err := checkTarget(request.URL.Host, ""); err != nil {
			return nil, err
		}
	}
	return (&headed{base: s.base, headers: s.headers}).RoundTrip(request)
}

// headed adds this server's headers to every request.
//
// A RoundTripper is where this has to happen. StreamableClientTransport
// takes an *http.Client and drives the requests itself — there is no hook
// that sees each one on the way out — and the transport reconnects a dropped
// stream on its own, so a header set once at handshake time would go missing
// on exactly the retry that needed it.
type headed struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip copies the request before touching it, because the contract says
// a RoundTripper must not modify the one it is given.
func (h headed) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	for key, value := range h.headers {
		cloned.Header.Set(key, value)
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}
