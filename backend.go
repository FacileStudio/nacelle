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

	// CountTokens reports how many tokens this request would use if sent as
	// it is, without sending it. A backend that cannot support it — see
	// Capabilities.TokenCounting — returns an *Unsupported error rather than
	// a guess: an estimate from a tokenizer this package does not own is not
	// a number anyone should budget against.
	CountTokens(ctx context.Context, request Request) (int64, error)
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

	// Effort reports whether the backend accepts a reasoning depth, which
	// covers both spellings of it: an effort level and a token budget. No
	// backend here takes one without the other, so splitting this in two
	// would add a field that can only ever agree with its neighbour.
	Effort bool

	// MinBudget is the smallest Thinking.Budget this backend's API will
	// take, or zero when it has no floor to report.
	//
	// A number rather than a bool because the refusal is only useful if it
	// says what to change to. Anthropic documents 1024 and rejects less;
	// the OpenRouter backend leaves this at zero and means it, because it
	// fronts hundreds of models whose floors are their own and a figure
	// invented here would refuse requests the gateway would have accepted.
	MinBudget int64

	// Cost reports whether Usage carries money rather than only tokens.
	// A backend that prices requests itself can fill it; one that does not
	// leaves Usage.Cost at zero and the caller prices the tokens.
	Cost bool

	// TokenCounting reports whether CountTokens is real rather than an
	// unconditional refusal. It takes a real request to a provider to know
	// exactly how many tokens a tokenizer nobody outside that provider owns
	// will produce, so a backend without an endpoint for it has nothing
	// honest to estimate with.
	TokenCounting bool
}

// Request is one run, fully described. A backend receives it already
// validated: the agent has checked it against Capabilities and filled every
// default, so a backend never has to guess what an empty field meant.
type Request struct {
	System        string
	Messages      []Message
	Tools         []Tool
	MCP           []mcp.Server
	Thinking      Thinking
	MaxTokens     int64
	MaxIterations int

	// Approve, if set, is asked before every local tool call runs. Nil
	// means every call runs unasked — see Approve's own doc comment.
	Approve Approve

	// Hooks run at fixed points around each local tool call. Nil means
	// none; see HookPoint.
	Hooks map[HookPoint][]Hook
}
