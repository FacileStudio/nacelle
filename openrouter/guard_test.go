package openrouter

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// The first call's arguments are whole and the second's are cut off mid-key,
// which is the shape that makes a length-truncated turn dangerous: the damage
// is at the end, so everything before it still parses and still runs.
const truncatedToolRequest = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"search","arguments":"{\"query\":\"one\"}"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"search","arguments":"{\"que"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10,"cost":0.0001}}

data: [DONE]

`

// function_call is the deprecated spelling of a tool request and carries no
// tool_calls array with it, so this turn asks for tools and hands over none.
const toolStopWithNoCalls = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","content":"calling"},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"function_call"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"cost":0.00001}}

data: [DONE]

`

// counting builds the tool the fixtures ask for, recording every execution so
// a test can prove one never happened rather than infer it from the events.
func counting(t *testing.T, runs *atomic.Int64) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		runs.Add(1)
		return "result for " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return tool
}

// MaxIterations is a blast radius, not a hint. A caller who caps a run at one
// request because the tools write files expects the same answer from every
// backend, and the Anthropic SDK checks the cap before it executes anything.
// Checking it at the top of the next iteration instead runs them first and
// reports StopIterations afterwards, which is the fence and the side effects.
func TestTheLastPermittedTurnDoesNotRunItsTools(t *testing.T) {
	var runs atomic.Int64
	backend, handler := serve(t, withToolCalls)
	events := collect(t, backend, nacelle.Request{
		System:        "s",
		Tools:         []nacelle.Tool{counting(t, &runs)},
		MaxIterations: 1,
	})

	if runs.Load() != 0 {
		t.Errorf("ran %d tools on the last permitted turn, want none", runs.Load())
	}
	if len(handler.requests) != 1 {
		t.Errorf("made %d requests, want the 1 the cap permits", len(handler.requests))
	}
	if calls, results := kinds(events, nacelle.KindToolCall), kinds(events, nacelle.KindToolResult); len(calls) != 2 || len(results) != 0 {
		t.Errorf("announced %d calls and %d results, want both calls announced and nothing run", len(calls), len(results))
	}

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Stop != nacelle.StopIterations || done[0].Usage.Total() == 0 {
		t.Errorf("done = %+v, want one StopIterations carrying the turn's usage", done)
	}
}

// A turn cut off by the output ceiling left its last call's arguments
// half-written, so the request is not what the model meant to send. The calls
// before the cut still parse, which is what makes running them worse than
// refusing: the tool executes with an invented tail and the run reports StopEnd.
func TestATruncatedTurnsToolsAreRefusedAndTheRunSaysSo(t *testing.T) {
	var runs atomic.Int64
	backend, handler := serve(t, truncatedToolRequest, finalAnswer)
	events := collect(t, backend, nacelle.Request{System: "s", Tools: []nacelle.Tool{counting(t, &runs)}})

	if runs.Load() != 0 {
		t.Errorf("ran %d tools from a truncated request, want none", runs.Load())
	}
	if len(handler.requests) != 1 {
		t.Errorf("made %d requests, want the run to stop at the truncated turn", len(handler.requests))
	}

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Stop != nacelle.StopMaxTokens {
		t.Fatalf("done = %+v, want one run ended on the output ceiling", done)
	}
	if done[0].Stop.Complete() {
		t.Error("a run built on a truncated tool request reports itself as a finished answer")
	}
}

// StopTools promises more turns and is documented as never being why a run
// ended, so a consumer that reads it on a KindDone waits for a turn that is
// never coming.
func TestATurnAskingForToolsWithNoneAttachedDoesNotEndOnStopTools(t *testing.T) {
	backend, _ := serve(t, toolStopWithNoCalls)
	events := collect(t, backend, nacelle.Request{System: "s"})

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Stop != nacelle.StopOther {
		t.Fatalf("done = %+v, want one run ended on StopOther", done)
	}
	if done[0].Stop.Complete() {
		t.Error("a turn that asked for tools it never sent reports a finished answer")
	}
}
