package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"
)

// The SDK's runner executes tool_use blocks and nothing else, so an MCP call
// is one no handler of ours ever runs. Nothing closed it, and event.go promises
// a KindToolResult carrying the same id — which is what a consumer's spinner is
// waiting on, on the backend whose headline capability is MCP.
func TestEveryMCPCallIsClosedByItsResult(t *testing.T) {
	backend := New(Config{Client: stub(t, sse(t, messageStart(),
		`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_use","id":"mcptoolu_1","name":"remote_search","server_name":"perception","input":{}}}`,
		arguments(t, 0, `{"query":"deploys"}`),
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"mcp_tool_result","tool_use_id":"mcptoolu_1","is_error":false,"content":[{"type":"text","text":"three events"}]}}`,
		`{"type":"content_block_stop","index":1}`,
		messageDelta("end_turn"), `{"type":"message_stop"}`))})

	events := collect(t, backend, nacelle.Request{MaxTokens: 1024, MaxIterations: 4})

	call, result := onlyCall(t, events)
	if result.ID != call.ID || result.Index != call.Index {
		t.Errorf("result = %+v, want the id and index of call %+v", result, call)
	}
	if result.Result != "three events" || result.Err != nil {
		t.Errorf("result = %+v, want the server's answer and no error", result)
	}
	if result.Input != call.Input {
		t.Errorf("result input = %q, call input = %q; they name one set of arguments", result.Input, call.Input)
	}
}

// A server that fails still answers, and a server that answers nothing at all
// still has to leave the call closed: the contract a consumer builds on is
// that a call ends, not that it ended well.
func TestAnMCPCallWithNoResultIsStillClosed(t *testing.T) {
	backend := New(Config{Client: stub(t, sse(t, messageStart(),
		`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_use","id":"mcptoolu_1","name":"remote_search","server_name":"perception","input":{}}}`,
		arguments(t, 0, `{"query":"deploys"}`),
		`{"type":"content_block_stop","index":0}`,
		messageDelta("end_turn"), `{"type":"message_stop"}`))})

	events := collect(t, backend, nacelle.Request{MaxTokens: 1024, MaxIterations: 4})

	call, result := onlyCall(t, events)
	if result.ID != call.ID || result.Index != call.Index {
		t.Errorf("result = %+v, want the id and index of call %+v", result, call)
	}
	if result.Err == nil {
		t.Errorf("result = %+v, want an error saying no result arrived", result)
	}
}

// A server reporting a failure is reported as one, so a consumer does not
// render an error message as the tool's answer.
func TestAFailedMCPCallCarriesTheError(t *testing.T) {
	backend := New(Config{Client: stub(t, sse(t, messageStart(),
		`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_use","id":"mcptoolu_1","name":"remote_search","server_name":"perception","input":{}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"mcp_tool_result","tool_use_id":"mcptoolu_1","is_error":true,"content":"the server refused"}}`,
		`{"type":"content_block_stop","index":1}`,
		messageDelta("end_turn"), `{"type":"message_stop"}`))})

	_, result := onlyCall(t, collect(t, backend, nacelle.Request{MaxTokens: 1024, MaxIterations: 4}))
	if result.Result != "the server refused" || result.Err == nil {
		t.Errorf("result = %+v, want the server's text and an error", result)
	}
}

// onlyCall returns the run's single call and the single result that closed it,
// which is the assertion itself: an unclosed call fails here.
func onlyCall(t *testing.T, events []nacelle.Event) (call, result *nacelle.ToolEvent) {
	t.Helper()
	for _, event := range events {
		switch event.Kind {
		case nacelle.KindToolCall:
			call = event.Tool
		case nacelle.KindToolResult:
			result = event.Tool
		}
	}
	if call == nil || result == nil {
		t.Fatalf("saw call=%+v result=%+v, want both; the events were %s", call, result, kinds(events))
	}
	return call, result
}

// kinds renders a run the way a consumer sees it, so a failure says what did
// arrive rather than only what did not.
func kinds(events []nacelle.Event) []nacelle.Kind {
	seen := make([]nacelle.Kind, 0, len(events))
	for _, event := range events {
		seen = append(seen, event.Kind)
	}
	return seen
}
