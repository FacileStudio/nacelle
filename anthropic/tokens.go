package anthropic

import (
	"context"

	"github.com/FacileStudio/nacelle"
	nmcp "github.com/FacileStudio/nacelle/mcp"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// CountTokens asks the API how many tokens this request would use, without
// sending it as a turn. Anthropic ships a dedicated endpoint for exactly this,
// which is what makes the number honest: nothing outside the provider owns the
// tokenizer, so anywhere else this package estimated it would be a guess
// dressed as a fact.
func (b *Backend) CountTokens(ctx context.Context, request nacelle.Request) (int64, error) {
	params := sdk.BetaMessageCountTokensParams{
		Model:    b.model,
		System:   sdk.BetaMessageCountTokensParamsSystemUnion{OfBetaTextBlockArray: []sdk.BetaTextBlockParam{{Text: request.System}}},
		Thinking: thinkingConfig(request.Thinking),
		Messages: toParams(request.Messages),
		Tools:    countableTools(request.Tools),
	}
	applyCountableMCP(&params, request.MCP)

	count, err := b.client.Beta.Messages.CountTokens(ctx, params)
	if err != nil {
		return 0, err
	}
	return count.InputTokens, nil
}

// countableTools renders nacelle tools in the shape the count-tokens endpoint
// wants, which is not the shape the runner executes: BetaTool is an interface
// the runner calls back into, and a call this package is never going to make
// has nothing to call back to. Sorted the same way adapt.go sorts them, or a
// count taken in a different order counts a different prefix than the one the
// request that follows it will actually send.
func countableTools(tools []nacelle.Tool) []sdk.BetaMessageCountTokensParamsToolUnion {
	if len(tools) == 0 {
		return nil
	}
	adapted := make([]sdk.BetaMessageCountTokensParamsToolUnion, 0, len(tools))
	for _, tool := range sortedByName(tools) {
		adapted = append(adapted, sdk.BetaMessageCountTokensParamsToolUnion{
			OfTool: &sdk.BetaToolParam{
				Name:        tool.Name(),
				Description: sdk.String(tool.Description()),
				InputSchema: toolInputSchema(tool),
			},
		})
	}
	return adapted
}

// applyCountableMCP declares the servers and the toolsets that reach them, in
// the count-tokens endpoint's own params type — the mirror of applyMCP in
// mcp.go, which targets BetaMessageNewParams and cannot be reused directly
// against a different struct carrying the same fields under the same rules: a
// request declaring mcp_servers without a matching toolset entry in tools is
// rejected as malformed here exactly as it is on the real request.
func applyCountableMCP(params *sdk.BetaMessageCountTokensParams, servers []nmcp.Server) {
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
		params.Tools = append(params.Tools, sdk.BetaMessageCountTokensParamsToolUnion{
			OfMCPToolset: &sdk.BetaMCPToolsetParam{MCPServerName: server.Name},
		})
	}
}
