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
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":0}`,
			messageDelta("end_turn"), `{"type":"message_stop"}`),
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

// The runner re-encodes a decoded input before handing it to a tool, so the
// bytes a handler sees are not the bytes the model streamed. Matching on them
// raw would miss every call whose arguments the model did not write in sorted,
// compact form.
func TestArgumentsMatchWhateverSpellingTheyArriveIn(t *testing.T) {
	pending := newInvocations()
	pending.record(&nacelle.ToolEvent{ID: "toolu_1", Index: 3, Name: "echo", Input: `{ "b": 2, "a": 1 }`})

	call := pending.take("echo", []byte(`{"a":1,"b":2}`))
	if call.ID != "toolu_1" || call.Index != 3 {
		t.Errorf("call = %+v, want toolu_1 at 3; the re-encoded input did not match", call)
	}
	if again := pending.take("echo", []byte(`{"a":1,"b":2}`)); again.ID != "" {
		t.Errorf("call = %+v, want nothing; a claimed invocation was handed out twice", again)
	}
}
