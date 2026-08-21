// Package client runs MCP servers as subprocesses and hands their tools back
// as ordinary [nacelle.Tool] values.
//
// # Why it is a package of its own
//
// mcp.Server is a remote server the Claude API dials on our behalf, which
// needs no client at all. This is the other case, and the two share nothing
// but the acronym: a subprocess has an executable, an environment and a
// lifetime somebody has to end, where the connector has a URL and a bearer
// token. There is also a mechanical reason they cannot be one package. The
// root package imports mcp, so mcp cannot import it back, and a bridged tool
// that is not a nacelle.Tool is not usable by anything.
//
// # Why bridging beats a second tool path
//
// A tool from here is an ordinary local tool, and that is the whole design.
// It passes through Config.Approve like every other, it emits the same
// KindToolCall and KindToolResult events, and it works on both backends —
// including openrouter, which refuses Config.MCP outright under the
// capability rule and would otherwise get no MCP tools at all, whatever the
// transport. A parallel MCP-shaped path through the loop would have to
// reimplement each of those, slightly differently.
//
// # What is deliberately absent
//
// notifications/tools/list_changed is ignored. The tool set is fixed when a
// request is built, so a server that grows a tool mid-run has nowhere to put
// it, and honouring the notification would mean telling a model about a tool
// the request it is answering never carried.
//
// Streamable HTTP is the other transport MCP defines and the SDK already
// speaks it. It is absent because nothing needs it yet, not because it would
// not fit: only [Command] and the transport it builds are stdio-specific, and
// everything after Connect works on a session whatever produced it.
package client

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/FacileStudio/nacelle"
)

// Set is a live connection to every server Connect started, and the tools
// they expose.
type Set struct {
	sessions []*sdk.ClientSession
	tools    []nacelle.Tool
}

// Connect starts every command and collects the tools they expose.
//
// The caller owns the result and must Close it. Each server is a running
// subprocess and nothing else will reap it:
//
//	set, err := client.Connect(ctx, commands...)
//	if err != nil {
//		return err
//	}
//	defer set.Close()
//	cfg.Tools = append(cfg.Tools, set.Tools()...)
//
// One server that cannot start or handshake fails the whole call, and
// whatever is already running is closed on the way out — the close is joined
// onto the error rather than swallowed, because a server that then refuses to
// die is the caller's problem too and nothing else is holding a handle to it.
// Degrading to the servers that did come up would hand the model a tool set
// that changes shape between runs, and a tool that is quietly missing reads
// as a model refusing to work rather than as a server that is down.
//
// Everything that can be refused is refused here rather than at call time: an
// unusable schema, a composed name the APIs will not accept, two tools
// composing to the same name. A tool that fails the first time the model
// reaches for it has already cost a turn and has already told the model
// something false about what it can do.
func Connect(ctx context.Context, servers ...Server) (*Set, error) {
	if err := validate(servers); err != nil {
		return nil, err
	}

	set := &Set{}
	taken := make(map[string]bool)
	for _, server := range servers {
		if err := set.attach(ctx, server, taken); err != nil {
			return nil, errors.Join(err, set.Close())
		}
	}
	return set, nil
}

// validate refuses a command list before a single process is started.
//
// Mirrors mcp.Validate, and for the same reason: a typo in configuration
// should cost a clear error, not a half-started tree of subprocesses that
// then has to be torn down to report it.
func validate(servers []Server) error {
	seen := make(map[string]bool, len(servers))
	for _, server := range servers {
		name := server.details().name
		switch {
		case name == "":
			return fmt.Errorf("nacelle/mcp/client: a server has no name")
		case seen[name]:
			return fmt.Errorf("nacelle/mcp/client: two servers are named %q", name)
		}
		if err := server.check(); err != nil {
			return err
		}
		seen[name] = true
	}
	return nil
}

// attach starts one server, lists what it offers and bridges it.
//
// Whatever the server wrote to stderr before it failed is hung off the error,
// which is the difference between an operator reading the reason and an
// operator reading the protocol step that noticed it.
//
// The session is recorded before the tools are listed, so that a server which
// starts and then fails to answer tools/list is still closed by Close rather
// than left running with nobody holding a handle to it.
//
// The subprocess is built with [exec.Command] and not [exec.CommandContext]:
// ctx here bounds the handshake, and binding the process to it would kill
// every server the moment Connect returned.
func (s *Set) attach(ctx context.Context, server Server, taken map[string]bool) error {
	about := server.details()
	ctx, cancel := context.WithTimeout(ctx, about.timeout)
	defer cancel()

	transport, notes, err := server.dial()
	if err != nil {
		return err
	}

	impl := &sdk.Implementation{Name: "nacelle", Version: implementationVersion}
	session, err := sdk.NewClient(impl, nil).Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("nacelle/mcp/client: starting server %q: %w%s", about.name, err, notes.note())
	}
	s.sessions = append(s.sessions, session)

	bridged, err := bridge(ctx, session, about, taken)
	if err != nil {
		return fmt.Errorf("%w%s", err, notes.note())
	}
	s.tools = append(s.tools, bridged...)
	return nil
}

// Tools is every bridged tool, ready to go into Config.Tools.
//
// It returns no error because there is nothing left to fail: a schema that
// could not be represented and a name the APIs would reject were both refused
// by Connect, back when there was still a useful place to report them.
func (s *Set) Tools() []nacelle.Tool { return s.tools }

// Close shuts every session down and reaps every subprocess.
//
// Errors are joined rather than returned on the first one, because stopping
// early would leave the remaining servers running — which is the leak this
// method exists to prevent, arrived at by a different route.
func (s *Set) Close() error {
	failures := make([]error, 0, len(s.sessions))
	for _, session := range s.sessions {
		if err := session.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	s.sessions = nil
	s.tools = nil
	return errors.Join(failures...)
}
