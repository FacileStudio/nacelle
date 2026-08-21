package client

import (
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultCallTimeout bounds one tools/call, and the handshake that precedes
// the first one.
//
// It matches tools.DefaultCommandTimeout because it bounds the same kind of
// work: an MCP server that greps a tree or runs a build needs the room a
// local command needs. What the bound really buys is the failure it prevents.
// A server that accepts a call and never answers would otherwise hold the
// agent's goroutine for the life of the process, and the run reads as a model
// that stopped thinking rather than as a subprocess that stopped talking.
const DefaultCallTimeout = 2 * time.Minute

// Server is one MCP server this process opens a session to: a [Command] run
// as a subprocess, or a [Remote] reached over HTTP.
//
// The interface is sealed — every method is unexported, so nothing outside
// this package can implement it. That is deliberate. A transport is not a
// place for a consumer to be inventive: everything after the session exists
// is identical whichever one produced it, and the two that exist are the two
// the MCP specification defines for a client to speak.
//
// It is an interface rather than one struct with a mode field because the
// two halves share almost no configuration. A subprocess has an executable,
// arguments, an environment and somewhere for its stderr to go; an endpoint
// has a URL and headers. Folding them together would give every caller a
// struct where two thirds of the fields are wrong for the server they are
// describing, and would make "which fields does this one read" a thing to
// look up rather than a thing to see.
type Server interface {
	// details is everything about a server that does not depend on how it
	// is reached.
	details() details

	// check refuses a configuration that cannot work, before anything is
	// started. It lives on the type because what makes a server unusable
	// is exactly what differs between them: a subprocess with no
	// executable, an endpoint with no URL.
	check() error

	// dial builds the transport and the place a failure to start can
	// explain itself. The diagnostics are always non-nil and are empty for
	// a transport with nowhere for a server to write, which keeps the one
	// error path in attach from having to ask which kind it has.
	dial() (sdk.Transport, *diagnostics, error)
}

// details is the half of a server's configuration that every transport has
// in common: what its tools are called, which of them may be called, and how
// long any of it may take.
//
// It is one value rather than three methods because the three travel
// together — bridge needs all of them and cares about none of the rest — and
// because an interface that grows an accessor per field is one that has to
// be widened every time a field is added, on both implementations, for
// nothing.
type details struct {
	name    string
	allowed []string
	timeout time.Duration
}

// timeoutOr is the timeout a server asked for, or the default it did not.
func timeoutOr(requested time.Duration) time.Duration {
	if requested <= 0 {
		return DefaultCallTimeout
	}
	return requested
}
