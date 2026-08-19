// Package nacelle is the agent SDK for the Facile Suite: the model loop, the
// tool registry and the MCP wiring that every Facile agent needs and none of
// them should be re-writing.
//
// An agent is a loop around a model with tools attached. That loop is about
// two hundred lines, and it gets rewritten in every project that needs one,
// slightly differently, with a slightly different bug in the tool-result
// handling. This is the single version.
//
// Three properties are what make it embeddable, and none of them is
// negotiable. The loop returns events and never prints, so a backend streaming
// SSE, a terminal UI and a test are all consumers of one stream. Usage is
// reported on every turn, because comparing runs on cost is a reason this
// package exists. And the core knows nothing about any product: no documents,
// no repositories, no citations. A consumer that needs its own vocabulary in
// here has found a bug in the abstraction, not a missing feature.
package nacelle

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/FacileStudio/nacelle/mcp"
	"github.com/anthropics/anthropic-sdk-go"
)

// DefaultModel is what an agent runs on when Config leaves Model empty.
//
// It is a plain string rather than an SDK constant because the SDK has no
// typed constant for Claude Opus 5, and anthropic.Model is an alias for string
// precisely so a current model does not have to wait for one.
const DefaultModel = "claude-opus-5"

// DefaultMaxTokens is the per-turn output ceiling.
//
// Generous on purpose. Every request this package makes is streamed, so a
// large ceiling costs nothing in latency or timeouts, while a small one
// truncates an answer mid-sentence and buys a retry.
const DefaultMaxTokens = 32000

// Effort tunes how hard the model works, trading cost against quality.
//
// It replaces the fixed thinking budget older models took: budget_tokens is
// rejected outright by Claude Opus 5, and this is what took its place.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// Config describes an agent. Every field has a working default except System,
// which is the one thing this package cannot guess.
type Config struct {
	// Client is the Anthropic client. Leave it nil to build one from the
	// environment, which is what a service wants; set it to share a client,
	// point at a proxy, or hand a test a stub transport.
	Client *anthropic.Client

	// Model defaults to DefaultModel.
	Model string

	// System is the system prompt.
	System string

	// Effort defaults to the model's own default, which is "high".
	Effort Effort

	// Thinking shows a readable summary of the model's reasoning in the
	// stream as KindThinking events.
	//
	// Off by default, which matches the API: on current models the raw
	// chain of thought is never returned and the summary is opt-in. Turning
	// it on changes what is displayed, never what is billed — the model
	// thinks either way.
	Thinking bool

	// MaxTokens defaults to DefaultMaxTokens.
	MaxTokens int64

	// MaxIterations caps how many times the model may call tools and be
	// asked again. Zero means no cap, which is the SDK's default and is
	// only safe when every tool is read-only and cheap.
	MaxIterations int

	// Tools the model may call in this process.
	Tools []Tool

	// MCP servers the model may call tools on. These run on Anthropic's
	// side of the request, not ours.
	MCP []mcp.Server

	// Logger receives the few things worth recording that are not events.
	// Defaults to slog.Default().
	Logger *slog.Logger
}

// Agent runs a conversation to completion, streaming what happens.
//
// It is safe to reuse across conversations and safe to share between
// goroutines: it holds configuration, not state. A single run is not — Stream
// must be ranged from one goroutine.
type Agent struct {
	client *anthropic.Client
	params anthropic.BetaToolRunnerParams
	tools  []Tool
	logger *slog.Logger
}

// ErrNoSystemPrompt is returned by New when Config.System is empty.
//
// It is an error rather than a default because an agent with no system prompt
// is a general-purpose assistant wearing a product's name, and that is never
// what the caller meant.
var ErrNoSystemPrompt = errors.New("nacelle: a system prompt is required")

// New builds an agent. It fails rather than degrading: a half-configured agent
// that answers plausibly is worse than one that refuses to start.
func New(cfg Config) (*Agent, error) {
	if cfg.System == "" {
		return nil, ErrNoSystemPrompt
	}
	if err := validateMCP(cfg.MCP); err != nil {
		return nil, err
	}

	cfg = cfg.resolved()
	params := cfg.params()
	applyMCP(&params.BetaMessageNewParams, cfg.MCP)

	return &Agent{
		client: cfg.Client,
		params: params,
		tools:  cfg.Tools,
		logger: cfg.Logger,
	}, nil
}

// resolved fills in every default, so nothing downstream has to ask twice
// whether a field was set.
func (c Config) resolved() Config {
	if c.Client == nil {
		built := anthropic.NewClient()
		c.Client = &built
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	return c
}

// params is the request every turn of a run is made from. Only the messages
// change between turns, and the runner owns those.
func (c Config) params() anthropic.BetaToolRunnerParams {
	params := anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(c.Model),
			MaxTokens: c.MaxTokens,
			System:    []anthropic.BetaTextBlockParam{{Text: c.System}},
			Thinking:  thinkingConfig(c.Thinking),
		},
		MaxIterations: c.MaxIterations,
	}
	if c.Effort != "" {
		params.OutputConfig = anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffort(c.Effort),
		}
	}
	return params
}

// thinkingConfig asks for adaptive thinking, with the summary shown or not.
//
// Adaptive is the only mode current models accept — a fixed budget_tokens is a
// 400 — and it is on by default on Claude Opus 5 whether or not it is named
// here. Naming it makes the display setting reachable, which is the only part
// a caller actually controls.
func thinkingConfig(visible bool) anthropic.BetaThinkingConfigParamUnion {
	adaptive := anthropic.BetaThinkingConfigAdaptiveParam{}
	if visible {
		adaptive.Display = anthropic.BetaThinkingConfigAdaptiveDisplaySummarized
	}
	return anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &adaptive}
}

// validateMCP refuses a server list that cannot work.
//
// Duplicate names are the interesting case: the toolset entry in the request
// finds a server by name, so two servers sharing one makes the second
// unreachable in a way that looks like the server being down.
func validateMCP(servers []mcp.Server) error {
	seen := make(map[string]bool, len(servers))
	for _, server := range servers {
		switch {
		case server.Name == "":
			return fmt.Errorf("nacelle: an MCP server has no name")
		case server.URL == "":
			return fmt.Errorf("nacelle: MCP server %q has no URL", server.Name)
		case seen[server.Name]:
			return fmt.Errorf("nacelle: two MCP servers are named %q", server.Name)
		}
		seen[server.Name] = true
	}
	return nil
}

// applyMCP declares the servers and the toolsets that reach them.
//
// Both halves are required. A request carrying mcp_servers without a matching
// mcp_toolset entry in tools is rejected as a validation error, which reads
// like a malformed server definition and is not one.
func applyMCP(params *anthropic.BetaMessageNewParams, servers []mcp.Server) {
	if len(servers) == 0 {
		return
	}

	params.Betas = append(params.Betas, anthropic.AnthropicBetaMCPClient2025_11_20)

	for _, server := range servers {
		definition := anthropic.BetaRequestMCPServerURLDefinitionParam{
			Name: server.Name,
			URL:  server.URL,
		}
		if server.Token != "" {
			definition.AuthorizationToken = anthropic.String(server.Token)
		}
		if len(server.AllowedTools) > 0 {
			definition.ToolConfiguration = anthropic.BetaRequestMCPServerToolConfigurationParam{
				AllowedTools: server.AllowedTools,
			}
		}
		params.MCPServers = append(params.MCPServers, definition)
		params.Tools = append(params.Tools, anthropic.BetaToolUnionParamOfMCPToolset(server.Name))
	}
}
