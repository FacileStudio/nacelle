package nacelle

import (
	"errors"
	"testing"

	"github.com/FacileStudio/nacelle/mcp"
	"github.com/anthropics/anthropic-sdk-go"
)

func TestNewRequiresASystemPrompt(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrNoSystemPrompt) {
		t.Fatalf("New with no system prompt = %v, want ErrNoSystemPrompt", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	agent, err := New(Config{System: "be useful"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := string(agent.params.Model); got != DefaultModel {
		t.Errorf("model = %q, want %q", got, DefaultModel)
	}
	if agent.params.MaxTokens != DefaultMaxTokens {
		t.Errorf("max tokens = %d, want %d", agent.params.MaxTokens, DefaultMaxTokens)
	}
	if agent.params.Thinking.OfAdaptive == nil {
		t.Error("thinking is not adaptive; a fixed budget is rejected by current models")
	}
}

// A duplicate name makes the second server unreachable rather than failing
// loudly, because the toolset entry in the request finds a server by name.
func TestNewRejectsUnusableMCPServers(t *testing.T) {
	cases := map[string][]mcp.Server{
		"no name":        {{URL: "https://example.test/mcp"}},
		"no url":         {{Name: "perception"}},
		"duplicate name": {{Name: "perception", URL: "https://a.test"}, {Name: "perception", URL: "https://b.test"}},
	}
	for name, servers := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(Config{System: "s", MCP: servers}); err == nil {
				t.Fatal("New accepted an unusable MCP server list")
			}
		})
	}
}

// Both halves or the API rejects the request: mcp_servers alone is a
// validation error, and it does not read like one.
func TestMCPDeclaresServersAndToolsets(t *testing.T) {
	agent, err := New(Config{
		System: "s",
		MCP: []mcp.Server{{
			Name:         "perception",
			URL:          "https://perception.facile.studio/api/mcp",
			Token:        "secret",
			AllowedTools: []string{"search_events"},
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if len(agent.params.MCPServers) != 1 {
		t.Fatalf("declared %d servers, want 1", len(agent.params.MCPServers))
	}
	server := agent.params.MCPServers[0]
	if server.Name != "perception" || server.URL != "https://perception.facile.studio/api/mcp" {
		t.Errorf("server = %+v, want the configured name and URL", server)
	}
	if server.AuthorizationToken.Value != "secret" {
		t.Error("the bearer token did not reach the server definition")
	}
	if len(server.ToolConfiguration.AllowedTools) != 1 {
		t.Error("the allowed-tools restriction was dropped")
	}
}

// mcp_servers on its own is rejected as a validation error, which does not
// read like a missing toolset and is one.
func TestMCPDeclaresTheToolsetAndTheBeta(t *testing.T) {
	agent, err := New(Config{
		System: "s",
		MCP:    []mcp.Server{{Name: "perception", URL: "https://perception.facile.studio/api/mcp"}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if len(agent.params.Tools) != 1 || agent.params.Tools[0].OfMCPToolset == nil {
		t.Fatal("no mcp_toolset entry; the request would be rejected as malformed")
	}

	var declared bool
	for _, beta := range agent.params.Betas {
		if beta == anthropic.AnthropicBetaMCPClient2025_11_20 {
			declared = true
		}
	}
	if !declared {
		t.Error("the MCP client beta was not declared")
	}
}

func TestNoMCPLeavesTheRequestUntouched(t *testing.T) {
	agent, err := New(Config{System: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(agent.params.MCPServers) != 0 || len(agent.params.Tools) != 0 || len(agent.params.Betas) != 0 {
		t.Error("an agent with no MCP servers still declared MCP")
	}
}

func TestUsageAccumulates(t *testing.T) {
	first := Usage{InputTokens: 10, OutputTokens: 3, CacheReadTokens: 1}
	second := Usage{InputTokens: 5, OutputTokens: 7, CacheCreationTokens: 2}

	sum := first.Add(second)
	want := Usage{InputTokens: 15, OutputTokens: 10, CacheReadTokens: 1, CacheCreationTokens: 2}
	if sum != want {
		t.Errorf("sum = %+v, want %+v", sum, want)
	}
	if sum.Total() != 28 {
		t.Errorf("total = %d, want 28", sum.Total())
	}
}

func TestConversationRolesSurvive(t *testing.T) {
	params := toParams([]Message{
		{Text: "what happened?"},
		{Assistant: true, Text: "a deploy"},
	})
	if len(params) != 2 {
		t.Fatalf("converted %d messages, want 2", len(params))
	}
	if params[0].Role != anthropic.BetaMessageParamRoleUser {
		t.Errorf("first role = %q, want user", params[0].Role)
	}
	if params[1].Role != anthropic.BetaMessageParamRoleAssistant {
		t.Errorf("second role = %q, want assistant", params[1].Role)
	}
}
