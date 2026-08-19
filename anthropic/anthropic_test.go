package anthropic

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
	nmcp "github.com/FacileStudio/nacelle/mcp"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// Cost is absent because the Anthropic API returns tokens and prices nothing,
// so the caller multiplies.
func TestCapabilitiesCoverEverythingButCost(t *testing.T) {
	can := New(Config{}).Capabilities()
	if !can.MCP || !can.Thinking || !can.Effort {
		t.Errorf("capabilities = %+v, want MCP, thinking and effort", can)
	}
	if can.Cost {
		t.Error("the backend claims to report cost; the Anthropic API returns tokens only")
	}
}

func TestThinkingIsAlwaysAdaptive(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{})
	if params.Thinking.OfAdaptive == nil {
		t.Fatal("thinking is not adaptive; a fixed token budget is rejected by current models")
	}
	if params.Thinking.OfAdaptive.Display != "" {
		t.Error("the reasoning summary is on by default; it should be opt-in")
	}

	visible := New(Config{}).params(nacelle.Request{Thinking: true})
	if visible.Thinking.OfAdaptive.Display != sdk.BetaThinkingConfigAdaptiveDisplaySummarized {
		t.Error("asking for thinking did not request the summary")
	}
}

// Both halves or the API rejects the request: mcp_servers without a matching
// mcp_toolset is a validation error that does not read like a missing toolset.
func TestMCPDeclaresServersToolsetsAndTheBeta(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{
		MCP: []nmcp.Server{{
			Name:         "perception",
			URL:          "https://perception.facile.studio/api/mcp",
			Token:        "secret",
			AllowedTools: []string{"search_events"},
		}},
	})

	if len(params.MCPServers) != 1 {
		t.Fatalf("declared %d servers, want 1", len(params.MCPServers))
	}
	server := params.MCPServers[0]
	if server.Name != "perception" || server.AuthorizationToken.Value != "secret" {
		t.Errorf("server = %+v, want the configured name and token", server)
	}
	if len(server.ToolConfiguration.AllowedTools) != 1 {
		t.Error("the allowed-tools restriction was dropped")
	}

	if len(params.Tools) != 1 || params.Tools[0].OfMCPToolset == nil {
		t.Fatal("no mcp_toolset entry; the request would be rejected as malformed")
	}

	var declared bool
	for _, beta := range params.Betas {
		if beta == sdk.AnthropicBetaMCPClient2025_11_20 {
			declared = true
		}
	}
	if !declared {
		t.Error("the MCP client beta was not declared")
	}
}

func TestNoMCPLeavesTheRequestUntouched(t *testing.T) {
	params := New(Config{}).params(nacelle.Request{})
	if len(params.MCPServers) != 0 || len(params.Tools) != 0 || len(params.Betas) != 0 {
		t.Error("a request with no MCP servers still declared MCP")
	}
}

func TestConversationRolesSurvive(t *testing.T) {
	params := toParams([]nacelle.Message{
		nacelle.UserText("what happened?"),
		nacelle.AssistantText("a deploy"),
	})
	if len(params) != 2 {
		t.Fatalf("converted %d messages, want 2", len(params))
	}
	if params[0].Role != sdk.BetaMessageParamRoleUser || params[1].Role != sdk.BetaMessageParamRoleAssistant {
		t.Errorf("roles = %q/%q, want user then assistant", params[0].Role, params[1].Role)
	}
}

// The runner needs the schema in its own shape, and a tool whose properties
// were dropped is a tool the model calls with nothing in it.
func TestToolSchemaSurvivesTheAdaptation(t *testing.T) {
	type input struct {
		Query string `json:"query" jsonschema:"required,description=What to look for"`
	}
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in input) (string, error) {
		return in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	adapted := adapt([]nacelle.Tool{tool}, &nacelle.ToolSink{}, newInvocations())
	if len(adapted) != 1 {
		t.Fatalf("adapted %d tools, want 1", len(adapted))
	}
	schema := adapted[0].InputSchema()
	properties, ok := schema.Properties.(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v, want the reflected object", schema.Properties)
	}
	if _, found := properties["query"]; !found {
		t.Errorf("properties = %#v, want a query field", properties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "query" {
		t.Errorf("required = %#v, want query alone", schema.Required)
	}
}
