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
	nmcp "github.com/FacileStudio/nacelle/mcp"

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

// Capabilities reports everything: this is the backend the others are measured
// against. Cost is absent because the API prices nothing in its responses — it
// returns tokens, and the caller multiplies.
func (b *Backend) Capabilities() nacelle.Capabilities {
	return nacelle.Capabilities{MCP: true, Thinking: true, Effort: true}
}

// Model is the model id this backend was built for.
func (b *Backend) Model() string { return b.model }

// params is the request every turn of a run is made from. Only the messages
// change between turns, and the runner owns those.
func (b *Backend) params(request nacelle.Request) sdk.BetaToolRunnerParams {
	params := sdk.BetaToolRunnerParams{
		BetaMessageNewParams: sdk.BetaMessageNewParams{
			Model:     sdk.Model(b.model),
			MaxTokens: request.MaxTokens,
			System:    []sdk.BetaTextBlockParam{{Text: request.System}},
			Thinking:  thinkingConfig(request.Thinking),
			Messages:  toParams(request.Messages),
		},
		MaxIterations: request.MaxIterations,
	}
	if request.Effort != "" {
		params.OutputConfig = sdk.BetaOutputConfigParam{
			Effort: sdk.BetaOutputConfigEffort(request.Effort),
		}
	}
	applyMCP(&params.BetaMessageNewParams, request.MCP)
	return params
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

// applyMCP declares the servers and the toolsets that reach them.
//
// Both halves are required. A request carrying mcp_servers without a matching
// mcp_toolset entry in tools is rejected as a validation error, which reads
// like a malformed server definition and is not one.
func applyMCP(params *sdk.BetaMessageNewParams, servers []nmcp.Server) {
	if len(servers) == 0 {
		return
	}

	params.Betas = append(params.Betas, sdk.AnthropicBetaMCPClient2025_11_20)

	for _, server := range servers {
		definition := sdk.BetaRequestMCPServerURLDefinitionParam{Name: server.Name, URL: server.URL}
		if server.Token != "" {
			definition.AuthorizationToken = sdk.String(server.Token)
		}
		if len(server.AllowedTools) > 0 {
			definition.ToolConfiguration = sdk.BetaRequestMCPServerToolConfigurationParam{
				AllowedTools: server.AllowedTools,
			}
		}
		params.MCPServers = append(params.MCPServers, definition)
		params.Tools = append(params.Tools, sdk.BetaToolUnionParamOfMCPToolset(server.Name))
	}
}

// toParams converts a conversation into the SDK's message shape.
func toParams(conversation []nacelle.Message) []sdk.BetaMessageParam {
	params := make([]sdk.BetaMessageParam, 0, len(conversation))
	for _, message := range conversation {
		role := sdk.BetaMessageParamRoleUser
		if message.Assistant {
			role = sdk.BetaMessageParamRoleAssistant
		}
		params = append(params, sdk.BetaMessageParam{
			Role:    role,
			Content: []sdk.BetaContentBlockParamUnion{sdk.NewBetaTextBlock(message.Text)},
		})
	}
	return params
}
