// Package nacelle is the agent SDK for the Facile Suite: the model loop, the
// tool registry and the MCP wiring that every Facile agent needs and none of
// them should be re-writing.
//
// An agent is a loop around a model with tools attached. That loop is about
// two hundred lines, and it gets rewritten in every project that needs one,
// slightly differently, with a slightly different bug in the tool-result
// handling. This is the single version.
//
// Four properties are what make it embeddable, and none of them is negotiable.
// The loop returns events and never prints, so a backend streaming SSE, a
// terminal UI and a test are all consumers of one stream. Usage is reported on
// every turn, because comparing runs on cost is a reason this package exists.
// Backends declare what they support and an agent that asks for more is
// refused at construction rather than quietly running with less. And the core
// knows nothing about any product: no documents, no repositories, no
// citations. A consumer that needs its own vocabulary in here has found a bug
// in the abstraction, not a missing feature.
package nacelle

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/FacileStudio/nacelle/mcp"
)

// DefaultMaxTokens is the per-turn output ceiling.
//
// Generous on purpose. Every request this package makes is streamed, so a
// large ceiling costs nothing in latency or timeouts, while a small one
// truncates an answer mid-sentence and buys a retry.
const DefaultMaxTokens = 32000

// Effort tunes how hard the model works, trading cost against quality.
//
// It replaces the fixed thinking budget older models took: a token budget is
// rejected outright by current Anthropic models, and this is what took its
// place. A backend that does not support it says so in its Capabilities.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// Config describes an agent. Backend and System are required; everything else
// has a working default.
type Config struct {
	// Backend is the model this agent runs on. There is no default: a
	// package that picks one for you is a package that hides the most
	// consequential decision in the configuration.
	Backend Backend

	// System is the system prompt.
	System string

	// Effort defaults to the backend's own default.
	Effort Effort

	// Thinking streams the model's reasoning as KindThinking events.
	//
	// Off by default, which matches the APIs: the raw chain of thought is
	// never returned and a readable summary is opt-in. Turning it on
	// changes what is displayed, never what is billed — the model thinks
	// either way.
	Thinking bool

	// MaxTokens defaults to DefaultMaxTokens.
	MaxTokens int64

	// MaxIterations caps how many times the model is asked, so a value of
	// N permits N requests and the tool rounds between them. Zero means no
	// cap, which is only safe when every tool is read-only and cheap.
	//
	// Reaching it is unfinished work rather than a failure: the run ends
	// with a KindDone carrying everything it cost and a Stop of
	// StopIterations. The last turn asked for tools that were never run, so
	// there is no answer built on them — check Stop before presenting one.
	MaxIterations int

	// Tools the model may call in this process.
	Tools []Tool

	// MCP servers the model may call tools on. These run on the backend's
	// side of the request, not ours, and only some backends can reach them.
	MCP []mcp.Server

	// Logger receives the few things worth recording that are not events.
	// Defaults to slog.Default().
	Logger *slog.Logger
}

// Agent runs a conversation to completion, streaming what happens.
//
// It is safe to reuse across conversations and safe to share between
// goroutines: it holds configuration, not state. A single run is not — a
// sequence returned by Stream must be ranged from one goroutine.
type Agent struct {
	backend Backend
	request Request
	logger  *slog.Logger
}

var (
	// ErrNoBackend is returned by New when Config.Backend is nil.
	ErrNoBackend = errors.New("nacelle: a backend is required")

	// ErrNoSystemPrompt is returned by New when Config.System is empty.
	//
	// It is an error rather than a default because an agent with no system
	// prompt is a general-purpose assistant wearing a product's name, and
	// that is never what the caller meant.
	ErrNoSystemPrompt = errors.New("nacelle: a system prompt is required")
)

// Unsupported reports that the backend cannot do something the config asked
// for. It is returned by New rather than swallowed, which is the whole point
// of Capabilities: losing MCP tools silently looks like a model that will not
// use them, and that is a bad afternoon.
type Unsupported struct {
	Backend string
	Feature string
}

func (e *Unsupported) Error() string {
	return fmt.Sprintf("nacelle: backend %q does not support %s", e.Backend, e.Feature)
}

// New builds an agent. It fails rather than degrading: a half-configured agent
// that answers plausibly is worse than one that refuses to start.
func New(cfg Config) (*Agent, error) {
	if cfg.Backend == nil {
		return nil, ErrNoBackend
	}
	if cfg.System == "" {
		return nil, ErrNoSystemPrompt
	}
	if err := validateTools(cfg.Tools); err != nil {
		return nil, err
	}
	if err := mcp.Validate(cfg.MCP); err != nil {
		return nil, err
	}
	if err := supports(cfg); err != nil {
		return nil, err
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{backend: cfg.Backend, logger: logger, request: cfg.request()}, nil
}

// request is the run this config describes, with every default filled, so a
// backend never has to guess what an empty field meant.
func (c Config) request() Request {
	maxTokens := c.MaxTokens
	if maxTokens == 0 {
		maxTokens = DefaultMaxTokens
	}
	return Request{
		System:        c.System,
		Tools:         c.Tools,
		MCP:           c.MCP,
		Effort:        c.Effort,
		Thinking:      c.Thinking,
		MaxTokens:     maxTokens,
		MaxIterations: c.MaxIterations,
	}
}

// Backend returns the backend this agent runs on, so a caller can report which
// model answered without having kept the value it passed in.
func (a *Agent) Backend() Backend { return a.backend }

// supports refuses a config the backend cannot honour.
func supports(cfg Config) error {
	can := cfg.Backend.Capabilities()
	name := cfg.Backend.Name()

	switch {
	case len(cfg.MCP) > 0 && !can.MCP:
		return &Unsupported{Backend: name, Feature: "MCP servers"}
	case cfg.Thinking && !can.Thinking:
		return &Unsupported{Backend: name, Feature: "streamed thinking"}
	case cfg.Effort != "" && !can.Effort:
		return &Unsupported{Backend: name, Feature: "effort levels"}
	}
	return nil
}

// validateTools refuses a tool set the model could not use unambiguously.
func validateTools(tools []Tool) error {
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name := tool.Name()
		if name == "" {
			return fmt.Errorf("nacelle: a tool has no name")
		}
		if seen[name] {
			return fmt.Errorf("nacelle: two tools are named %q", name)
		}
		seen[name] = true
	}
	return nil
}
