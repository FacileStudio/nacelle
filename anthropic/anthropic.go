// Package anthropic runs a nacelle agent on the Anthropic API.
//
// It is the full-capability backend, and the reason is not the model: the
// Anthropic API ships the agent loop in its SDK and connects to remote MCP
// servers on its own side of the request. Both of those are transport
// features, not model features, and a backend speaking another schema has to
// reimplement the first and cannot have the second at all.
package anthropic

import (
	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// DefaultModel is what this backend runs on when Config leaves Model empty.
//
// A plain string rather than an SDK constant: the SDK has no typed constant
// for Claude Opus 5, and sdk.Model is an alias for string precisely so a
// current model does not have to wait for one.
const DefaultModel = "claude-opus-5"

// Config describes the backend.
type Config struct {
	// Client is the Anthropic client. Leave it nil to build one from the
	// environment, which is what a service wants; set it to share a client,
	// point at a proxy, or hand a test a stub transport.
	Client *sdk.Client

	// Model defaults to DefaultModel.
	Model string
}

// Backend runs agents on the Anthropic API.
type Backend struct {
	client *sdk.Client
	model  string
}

var _ nacelle.Backend = (*Backend)(nil)

// New builds the backend.
func New(cfg Config) *Backend {
	client := cfg.Client
	if client == nil {
		built := sdk.NewClient()
		client = &built
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	return &Backend{client: client, model: model}
}

// Name identifies the backend.
func (b *Backend) Name() string { return "anthropic" }

// Capabilities reports everything but Cost: this is the backend the others
// are measured against. Cost is absent because the API prices nothing in its
// responses — it returns tokens, and the caller multiplies. TokenCounting is
// present because the API ships a dedicated endpoint for it — see tokens.go.
func (b *Backend) Capabilities() nacelle.Capabilities {
	return nacelle.Capabilities{MCP: true, Thinking: true, Effort: true, TokenCounting: true}
}

// Model is the model id this backend was built for.
func (b *Backend) Model() string { return b.model }

// params is the request every turn of a run is made from. Only the messages
// change between turns, and the runner owns those.
//
// CacheControl is set at the top level, which asks the API to mark the last
// cacheable block itself and move that mark forward as the conversation grows.
// Doing it there rather than per block is not a shortcut: the runner owns the
// message slice, so the individual blocks are not ours to annotate, and the
// server-side placement is the pattern the documentation recommends anyway.
func (b *Backend) params(request nacelle.Request) sdk.BetaToolRunnerParams {
	params := sdk.BetaToolRunnerParams{
		BetaMessageNewParams: sdk.BetaMessageNewParams{
			Model:     b.model,
			MaxTokens: request.MaxTokens,
			System:    []sdk.BetaTextBlockParam{{Text: request.System}},
			Thinking:  thinkingConfig(request.Thinking),
			Messages:  toParams(request.Messages),
		},
		MaxIterations: request.MaxIterations,
	}
	if amortises(request) {
		params.CacheControl = sdk.NewBetaCacheControlEphemeralParam()
	}
	if request.Effort != "" {
		params.OutputConfig = sdk.BetaOutputConfigParam{
			Effort: sdk.BetaOutputConfigEffort(request.Effort),
		}
	}
	applyMCP(&params.BetaMessageNewParams, request.MCP)
	return params
}

// amortises reports whether this run will make a second request sharing a
// prefix with the first, which is the only condition under which a cache
// breakpoint pays for itself.
//
// A cache write costs 1.25x a plain input token and a read costs 0.1x, so a
// prefix written once and read once is already ahead, and over ten turns it is
// the difference between paying for the system prompt and every tool schema
// once and paying for them ten times. That case is worth taking and it is not
// every case. A Config with no tools and no conversation behind it makes
// exactly one API call, and a breakpoint there buys a cache entry nothing will
// ever read: a 5k-token one-shot pays 1.25x its input for nothing. Charging
// every caller a quarter extra to spare this package a condition is not a
// trade-off worth defending, so the condition is here.
//
// Two things predict the second request. Local tools mean the SDK's runner
// resends the whole conversation on every iteration, so any run that calls one
// has already made it. A conversation handed in means an earlier run wrote
// this prefix and this one is the read. MCP servers deliberately do not count:
// they run on Anthropic's side within a single response, so a run whose only
// tools are remote still makes one request — and if it also has local tools,
// those already said yes.
func amortises(request nacelle.Request) bool {
	return len(request.Tools) > 0 || len(request.Messages) > 0
}

// thinkingConfig asks for adaptive thinking, with the summary shown or not.
//
// Adaptive is the only mode current models accept — a fixed token budget is a
// 400 — and it is on by default on Claude Opus 5 whether or not it is named
// here. Naming it makes the display setting reachable, which is the only part
// a caller actually controls.
func thinkingConfig(visible bool) sdk.BetaThinkingConfigParamUnion {
	adaptive := sdk.BetaThinkingConfigAdaptiveParam{}
	if visible {
		adaptive.Display = sdk.BetaThinkingConfigAdaptiveDisplaySummarized
	}
	return sdk.BetaThinkingConfigParamUnion{OfAdaptive: &adaptive}
}
