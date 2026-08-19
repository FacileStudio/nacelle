package anthropic

import (
	"fmt"
	"strings"

	"github.com/FacileStudio/nacelle"
	nmcp "github.com/FacileStudio/nacelle/mcp"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

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

// remoteResult closes the MCP call this result block answers.
//
// It has to be done here because nothing else can. An MCP tool runs on
// Anthropic's side, so the SDK's runner never sees it — executeTools filters
// on tool_use alone — and its answer comes back as a content block on the same
// turn. event.go promises that a KindToolResult carries the id of the
// KindToolCall it answers, and without this every MCP call this backend
// reports is a call a consumer waits on forever, on the backend whose headline
// capability is MCP.
func (c *callTracker) remoteResult(block sdk.BetaRawContentBlockStartEventContentBlockUnion) []nacelle.Event {
	call, found := c.remote[block.ToolUseID]
	if !found {
		return nil
	}
	delete(c.remote, block.ToolUseID)

	answer := &nacelle.ToolEvent{
		ID: call.ID, Index: call.Index, Name: call.Name, Input: call.Input,
		Result: resultText(block.Content),
	}
	if block.IsError {
		answer.Err = fmt.Errorf("nacelle/anthropic: the MCP server failed %q: %s", call.Name, answer.Result)
	}
	return []nacelle.Event{{Kind: nacelle.KindToolResult, Tool: answer}}
}

// resultText is what the MCP server said, in the one shape a consumer reads.
//
// The API spells a result either as a bare string or as a list of text blocks,
// which is one thing spelled twice. The list is joined rather than reduced to
// its first block, because a server that answers in several parts is answering
// once.
func resultText(content sdk.BetaRawContentBlockStartEventContentBlockUnionContent) string {
	if content.OfString != "" {
		return content.OfString
	}
	var joined strings.Builder
	for _, block := range content.OfBetaMCPToolResultBlockContent {
		joined.WriteString(block.Text)
	}
	return joined.String()
}
