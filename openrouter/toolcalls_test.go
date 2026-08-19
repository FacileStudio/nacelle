package openrouter

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

const withToolCalls = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\":"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"query\":\"two\"}"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"one\"}"}}]},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls","native_finish_reason":"tool_use"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10,"cost":0.0001}}

data: [DONE]

`

const finalAnswer = `data: {"id":"g2","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":null}]}

data: {"id":"g2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"g2","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":1,"total_tokens":21,"cost":0.0002}}

data: [DONE]

`

type searchInput struct {
	Query string `json:"query" jsonschema:"required,description=What to look for"`
}

// Two requests are expected: the tool calls, then the answer built on their
// results. The schema has to go on the follow-up too, because OpenRouter
// validates it per call and a request that omits it is a different
// conversation. Cost is compared with a tolerance because it is a float64 and
// 0.0001 plus 0.0002 is not exactly 0.0003 in binary.
// assertBothCallsRan checks each call received its own fragmented arguments
// rather than the other's.
func assertBothCallsRan(t *testing.T, events []nacelle.Event) {
	t.Helper()
	results := kinds(events, nacelle.KindToolResult)
	if len(results) != 2 {
		t.Fatalf("ran %d tools, want 2", len(results))
	}

	got := map[string]string{}
	for _, event := range results {
		got[event.Tool.ID] = event.Tool.Result
	}
	if got["call_a"] != "result for one" {
		t.Errorf("call_a = %q, want its own fragmented arguments", got["call_a"])
	}
	if got["call_b"] != "result for two" {
		t.Errorf("call_b = %q, want its own arguments, not the other call's", got["call_b"])
	}
}

func TestParallelToolCallsAreRunAndFedBack(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "result for " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend, handler := serve(t, withToolCalls, finalAnswer)
	events := collect(t, backend, nacelle.Request{
		System:   "s",
		Messages: []nacelle.Message{{Text: "search twice"}},
		Tools:    []nacelle.Tool{tool},
	})

	assertBothCallsRan(t, events)

	if len(handler.requests) != 2 {
		t.Fatalf("made %d requests, want 2", len(handler.requests))
	}
	if _, sent := handler.requests[1]["tools"]; !sent {
		t.Error("the follow-up request dropped the tool schema")
	}

	total := kinds(events, nacelle.KindDone)[0].Usage
	if total.InputTokens != 25 {
		t.Errorf("input tokens = %d, want both turns accumulated", total.InputTokens)
	}
	if diff := total.Cost - 0.0003; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("cost = %v, want both turns accumulated to 0.0003", total.Cost)
	}
}

// A tool_call with no answering tool message is rejected by most providers on
// the next request, so a failure has to produce a message too.
func TestAFailingToolStillAnswersTheCall(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, searchInput) (string, error) {
		return "", io.ErrUnexpectedEOF
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend, handler := serve(t, withToolCalls, finalAnswer)
	collect(t, backend, nacelle.Request{System: "s", Tools: []nacelle.Tool{tool}})

	messages, _ := handler.requests[1]["messages"].([]any)
	var toolMessages int
	for _, message := range messages {
		if entry, ok := message.(map[string]any); ok && entry["role"] == "tool" {
			toolMessages++
		}
	}
	if toolMessages != 2 {
		t.Errorf("sent %d tool messages, want one per call even though both failed", toolMessages)
	}
}

func TestAnUnknownToolIsAnsweredNotDropped(t *testing.T) {
	backend, handler := serve(t, withToolCalls, finalAnswer)
	collect(t, backend, nacelle.Request{System: "s"})

	messages, _ := handler.requests[1]["messages"].([]any)
	var answered int
	for _, message := range messages {
		if entry, ok := message.(map[string]any); ok && entry["role"] == "tool" {
			answered++
			if content, _ := entry["content"].(string); !strings.Contains(content, "no tool named") {
				t.Errorf("content = %q, want it to say the tool is unavailable", content)
			}
		}
	}
	if answered != 2 {
		t.Errorf("answered %d calls, want both", answered)
	}
}

// assertCallsPrecedeResults checks every result answers a call the consumer
// was already told about, by ID.
func assertCallsPrecedeResults(t *testing.T, events []nacelle.Event) {
	t.Helper()
	announced := map[string]int{}
	for position, event := range events {
		switch event.Kind {
		case nacelle.KindToolCall:
			announced[event.Tool.ID] = position
		case nacelle.KindToolResult:
			if _, seen := announced[event.Tool.ID]; !seen {
				t.Errorf("the result for %q arrived with no call announced first", event.Tool.ID)
			}
		}
	}
	if len(announced) != 2 {
		t.Errorf("announced %d distinct calls, want 2", len(announced))
	}
}

// A tool running with no event to show it is the difference between a UI that
// says what the agent is doing and one that freezes. This backend drives the
// tool loop itself, so nothing else can announce the call: it has to emit
// KindToolCall before the work starts, with the ID that pairs it to its result
// and the index that carries the model's own ordering.
func TestEveryToolCallIsAnnouncedBeforeTheToolRuns(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "result for " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend, _ := serve(t, withToolCalls, finalAnswer)
	events := collect(t, backend, nacelle.Request{System: "s", Tools: []nacelle.Tool{tool}})

	calls := kinds(events, nacelle.KindToolCall)
	if len(calls) != 2 {
		t.Fatalf("announced %d calls, want 2", len(calls))
	}
	for wanted, event := range calls {
		if event.Tool.Index != wanted {
			t.Errorf("call %d carries index %d, want the model's own ordering", wanted, event.Tool.Index)
		}
		if event.Tool.ID == "" || event.Tool.Name != "search" || event.Tool.Input == "" {
			t.Errorf("call %d = %+v, want an id, a name and the raw arguments", wanted, *event.Tool)
		}
	}

	assertCallsPrecedeResults(t, events)
}
