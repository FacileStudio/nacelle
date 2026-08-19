package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"
)

// The Event contract says a KindToolResult carries the id of the call it
// answers, and that is the only thing a consumer can pair them by: tools run
// in parallel, so results arrive in the order they finish. An empty id makes
// pairing impossible, and the same tool called twice in one turn is where a
// name-only correlation would silently swap two answers.
func TestEveryToolResultNamesTheCallItAnswers(t *testing.T) {
	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
			arguments(t, 0, `{"text":"first"}`),
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_2","name":"echo","input":{}}}`,
			arguments(t, 1, `{"text":"second"}`),
			`{"type":"content_block_stop","index":1}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
		answeringTurn(t),
	)})

	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{echoTool(t)},
		MaxTokens:     1024,
		MaxIterations: 4,
	})

	calls := toolsOf(events, nacelle.KindToolCall)
	results := toolsOf(events, nacelle.KindToolResult)
	if len(calls) != 2 || len(results) != 2 {
		t.Fatalf("saw %d calls and %d results, want two of each", len(calls), len(results))
	}
	assertPaired(t, calls, results)
}

// assertPaired checks that every result names a call, sits at that call's
// position, and answers that call's arguments rather than its sibling's.
func assertPaired(t *testing.T, calls, results map[string]*nacelle.ToolEvent) {
	t.Helper()
	wanted := map[string]struct {
		index  int
		result string
	}{
		"toolu_1": {index: 0, result: "first"},
		"toolu_2": {index: 1, result: "second"},
	}
	for id, want := range wanted {
		call, made := calls[id]
		result, answered := results[id]
		if !made || !answered {
			t.Fatalf("call %q: made=%v answered=%v, want both; results = %v", id, made, answered, results)
		}
		if call.Index != want.index || result.Index != want.index {
			t.Errorf("call %q sits at %d and its result at %d, want %d for both", id, call.Index, result.Index, want.index)
		}
		if result.Result != want.result {
			t.Errorf("call %q was answered %q, want %q; the results were swapped", id, result.Result, want.result)
		}
	}
}

// toolsOf indexes one kind of tool event by id, which is only possible if the
// ids are there in the first place.
func toolsOf(events []nacelle.Event, kind nacelle.Kind) map[string]*nacelle.ToolEvent {
	found := map[string]*nacelle.ToolEvent{}
	for _, event := range events {
		if event.Kind == kind && event.Tool != nil && event.Tool.ID != "" {
			found[event.Tool.ID] = event.Tool
		}
	}
	return found
}

// An MCP call the runner will never execute must not sit in the registry,
// because nothing validates that a local tool and a remote one have different
// names. Same name, same arguments, and the local call's result ships the
// remote call's id and index — silently, and looking exactly like the swapped
// result the id exists to prevent.
func TestAnMCPCallCannotStealALocalCallsIdentity(t *testing.T) {
	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_use","id":"mcptoolu_1","name":"echo","server_name":"perception","input":{}}}`,
			arguments(t, 0, `{"text":"first"}`),
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"mcp_tool_result","tool_use_id":"mcptoolu_1","is_error":false,"content":"first"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
			arguments(t, 2, `{"text":"first"}`),
			`{"type":"content_block_stop","index":2}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
		answeringTurn(t),
	)})

	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{echoTool(t)},
		MaxTokens:     1024,
		MaxIterations: 4,
	})

	results := toolsOf(events, nacelle.KindToolResult)
	if len(results) != 2 {
		t.Fatalf("saw %d results, want one per call; the events were %s", len(results), kinds(events))
	}
	local, closed := results["toolu_1"]
	if !closed {
		t.Fatalf("the local call was answered under another id; results = %v", results)
	}
	if local.Index != 1 {
		t.Errorf("the local result sits at %d, want 1; it took the MCP call's position", local.Index)
	}
}

// The bytes a consumer sees on a call and on its result have to be one string.
// The runner re-encodes what it decoded before handing it to a handler, so
// reporting its bytes would show a change the model never made — and the
// OpenRouter backend reports the model's own string on both.
func TestACallAndItsResultReportOneSetOfArguments(t *testing.T) {
	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
			arguments(t, 0, `{ "text" : "spaced" }`),
			`{"type":"content_block_stop","index":0}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
		sse(t, messageStart(), messageDelta("end_turn"), `{"type":"message_stop"}`),
	)})

	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{echoTool(t)},
		MaxTokens:     1024,
		MaxIterations: 4,
	})

	call := toolsOf(events, nacelle.KindToolCall)["toolu_1"]
	result := toolsOf(events, nacelle.KindToolResult)["toolu_1"]
	if call == nil || result == nil {
		t.Fatalf("saw call=%+v result=%+v, want both", call, result)
	}
	if result.Input != call.Input {
		t.Errorf("call input = %q and result input = %q, want one string", call.Input, result.Input)
	}
}

// answeringTurn is the turn after the tools ran: the model says what it found
// and stops, which is what lets the runner finish rather than ask again.
func answeringTurn(t *testing.T) string {
	t.Helper()
	return sse(t, messageStart(),
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"content_block_stop","index":0}`,
		messageDelta("end_turn"), `{"type":"message_stop"}`)
}
