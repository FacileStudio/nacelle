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
	return nacelle.Capabilities{MCP: true, Thinking: true, Effort: true, MinBudget: 1024, TokenCounting: true}
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
	if effort, wanted := outputEffort(request.Thinking.Effort); wanted {
		params.OutputConfig = sdk.BetaOutputConfigParam{Effort: effort}
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

// thinkingConfig renders a request's thinking settings as the union the API
// takes, picking the variant that can express what was asked for.
//
// There are three variants because there are three different asks. EffortNone
// is the caller saying not to reason at all, and the disabled variant is the
// only one that can say it. A budget is a ceiling on reasoning tokens, and
// BudgetTokens exists nowhere else in the union, so a request carrying one has
// to travel as the enabled variant. Everything else is adaptive, which is what
// current models default to and the only mode they accept without a budget: a
// fixed budget handed to a model that does not take one comes back a 400.
//
// The display setting is asked for only when the consumer wants to watch, and
// it decides less than its name suggests. The API returns a summarized display
// by default, so the reasoning travels back either way, which is what
// nacelle.Thinking asks for: the model needs its own last thought on the turn
// after, and the tokens are billed whether or not they are returned. Show gates
// what a consumer is shown and nothing else. The disabled variant has no
// display setting, and loses nothing by it, because a run that asked for no
// reasoning has none to show.
// outputEffort is the level to put on the request, and whether to put one
// there at all.
//
// nacelle.Effort is the union of what the providers accept, and Anthropic
// takes five of the seven. The two it does not are answered here rather than
// cast straight through, because output_config.effort is a closed enum:
// "minimal" is not in it, and "none" would sit next to a thinking block that
// thinkingConfig has already switched off, which is a request contradicting
// itself.
//
// minimal becomes low rather than nothing. A caller asking for the least
// reasoning available and getting the model's own default would receive more
// of it, not less, and failing in that direction is worse than the small lie.
// Clamping is also what the other backend gets for free: measured against
// OpenRouter on 2026-08-23, a level a model does not advertise is clamped to
// one it does rather than refused, so doing it here is what keeps the two
// backends agreeing about what an unavailable level means.
func outputEffort(effort nacelle.Effort) (sdk.BetaOutputConfigEffort, bool) {
	switch effort {
	case "", nacelle.EffortNone:
		return "", false
	case nacelle.EffortMinimal:
		return sdk.BetaOutputConfigEffort(nacelle.EffortLow), true
	}
	return sdk.BetaOutputConfigEffort(effort), true
}

func thinkingConfig(t nacelle.Thinking) sdk.BetaThinkingConfigParamUnion {
	switch {
	case t.Effort == nacelle.EffortNone:
		disabled := sdk.NewBetaThinkingConfigDisabledParam()
		return sdk.BetaThinkingConfigParamUnion{OfDisabled: &disabled}
	case t.Budget > 0:
		enabled := sdk.BetaThinkingConfigEnabledParam{BudgetTokens: t.Budget}
		if t.Show {
			enabled.Display = sdk.BetaThinkingConfigEnabledDisplaySummarized
		}
		return sdk.BetaThinkingConfigParamUnion{OfEnabled: &enabled}
	default:
		adaptive := sdk.BetaThinkingConfigAdaptiveParam{}
		if t.Show {
			adaptive.Display = sdk.BetaThinkingConfigAdaptiveDisplaySummarized
		}
		return sdk.BetaThinkingConfigParamUnion{OfAdaptive: &adaptive}
	}
}
