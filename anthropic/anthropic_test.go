package anthropic

import (
	"context"
	"encoding/json"
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
		{Text: "what happened?"},
		{Assistant: true, Text: "a deploy"},
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

// A tool call arrives in pieces and must be reported whole: emitting it at
// content_block_start would report a call whose arguments are still empty.
func TestToolCallIsReportedOnceItsInputIsComplete(t *testing.T) {
	tracker := newCallTracker(newInvocations())

	if got := tracker.consume(raw(t, `{"type":"content_block_start","index":0,
		"content_block":{"type":"tool_use","id":"toolu_1","name":"search_events"}}`)); len(got) != 0 {
		t.Fatalf("the start of a tool call emitted %d events, want 0", len(got))
	}
	tracker.consume(argumentFragment(t, 0, `{"query":`))
	tracker.consume(argumentFragment(t, 0, `"deploys"}`))

	events := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	if len(events) != 1 || events[0].Kind != nacelle.KindToolCall {
		t.Fatalf("the end of a tool call emitted %+v, want one KindToolCall", events)
	}
	call := events[0].Tool
	if call.ID != "toolu_1" || call.Name != "search_events" || call.Input != `{"query":"deploys"}` {
		t.Errorf("tool = %+v, want the call reassembled whole", call)
	}
}

// A tool this process runs and one the API runs over MCP are the same event to
// a consumer.
func TestMCPToolCallsAreTrackedToo(t *testing.T) {
	tracker := newCallTracker(newInvocations())
	tracker.consume(raw(t, `{"type":"content_block_start","index":0,
		"content_block":{"type":"mcp_tool_use","id":"mcp_1","name":"get_entity"}}`))

	events := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	if len(events) != 1 || events[0].Tool.Name != "get_entity" {
		t.Fatalf("an MCP tool call was not tracked: %+v", events)
	}
}

// Two tools requested in one turn interleave on the wire, so the index is what
// keeps their arguments apart.
func TestConcurrentToolCallsDoNotMixTheirInput(t *testing.T) {
	tracker := newCallTracker(newInvocations())
	tracker.consume(raw(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"first"}}`))
	tracker.consume(raw(t, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"b","name":"second"}}`))
	tracker.consume(argumentFragment(t, 0, `{"a":1}`))
	tracker.consume(argumentFragment(t, 1, `{"b":2}`))

	first := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	second := tracker.consume(raw(t, `{"type":"content_block_stop","index":1}`))

	if len(first) != 1 || first[0].Tool.Input != `{"a":1}` {
		t.Errorf("first call = %+v, want its own arguments", first)
	}
	if len(second) != 1 || second[0].Tool.Input != `{"b":2}` {
		t.Errorf("second call = %+v, want its own arguments", second)
	}
}

func TestTextAndThinkingBecomeDeltas(t *testing.T) {
	tracker := newCallTracker(newInvocations())

	text := tracker.consume(raw(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`))
	if len(text) != 1 || text[0].Kind != nacelle.KindText || text[0].Text != "hello" {
		t.Errorf("text delta = %+v, want a KindText event carrying hello", text)
	}

	thinking := tracker.consume(raw(t, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`))
	if len(thinking) != 1 || thinking[0].Kind != nacelle.KindThinking || thinking[0].Text != "hmm" {
		t.Errorf("thinking delta = %+v, want a KindThinking event carrying hmm", thinking)
	}
}

// raw builds a stream event the way the wire does, so the test exercises the
// SDK's own decoding of the union rather than a hand-filled struct that could
// disagree with it.
func raw(t *testing.T, payload string) sdk.BetaRawMessageStreamEventUnion {
	t.Helper()
	var event sdk.BetaRawMessageStreamEventUnion
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("decoding %s: %v", payload, err)
	}
	return event
}

// argumentFragment is one slice of a tool call's JSON arguments, built through
// the encoder so a quote in the fragment cannot break the payload around it.
func argumentFragment(t *testing.T, index int, fragment string) sdk.BetaRawMessageStreamEventUnion {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": fragment},
	})
	if err != nil {
		t.Fatalf("building the delta: %v", err)
	}
	return raw(t, string(payload))
}
