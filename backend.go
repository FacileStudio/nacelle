package nacelle

import (
	"context"
	"iter"

	"github.com/FacileStudio/nacelle/mcp"
)

// Backend is a model this package can run an agent on.
//
// The seam is at the whole loop rather than at a single request, because the
// loop is exactly what differs. Anthropic ships one in its SDK and executes
// remote MCP servers on its own side of the request; an OpenAI-schema backend
// has neither and must drive the conversation itself. An interface at the
// request level would have forced the Anthropic path to give up the tested
// loop it gets for free, to look symmetrical with one that cannot have it.
type Backend interface {
	// Name identifies the backend in errors and logs.
	Name() string

	// Capabilities reports what this backend can actually do, so an agent
	// asking for something it lacks fails at construction rather than
	// quietly running with less.
	Capabilities() Capabilities

	// Stream runs the conversation to completion, yielding events.
	//
	// Implementations must end with a KindDone carrying the run's total
	// usage, or with an error. They must report tool results through
	// RunTool and a ToolSink so that every backend's stream looks the same
	// to a consumer.
	Stream(ctx context.Context, request Request) iter.Seq2[Event, error]
}

// Capabilities is what a backend supports.
//
// Every field is a feature a caller can ask for and be refused. The list is
// deliberately not a set of vague tiers: a consumer that needs MCP needs to
// know that specific thing is missing, not that the backend is "limited".
type Capabilities struct {
	// MCP reports whether the backend can reach remote MCP servers.
	//
	// On the Anthropic API this is a request parameter and the servers are
	// called from Anthropic's side. A backend without it would need a full
	// MCP client, which is a different piece of software.
	MCP bool

	// Thinking reports whether the backend can stream the model's
	// reasoning as KindThinking events.
	Thinking bool

	// Effort reports whether the backend accepts an effort level.
	Effort bool

	// Cost reports whether Usage carries money rather than only tokens.
	// A backend that prices requests itself can fill it; one that does not
	// leaves Usage.Cost at zero and the caller prices the tokens.
	Cost bool
}

// Request is one run, fully described. A backend receives it already
// validated: the agent has checked it against Capabilities and filled every
// default, so a backend never has to guess what an empty field meant.
type Request struct {
	System        string
	Messages      []Message
	Tools         []Tool
	MCP           []mcp.Server
	Effort        Effort
	Thinking      bool
	MaxTokens     int64
	MaxIterations int
}

// Message is one turn of the conversation so far.
type Message struct {
	// Assistant marks a message the model produced rather than the user.
	Assistant bool
	Text      string
}
