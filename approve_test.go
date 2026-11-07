package nacelle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// echoInput builds a tool that answers with its own name and the raw input it
// ran with, so a test can see exactly what reached it.
func echoInput(t *testing.T, name string) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewTool(name, "Echo", func(_ context.Context, in searchInput) (string, error) {
		return name + ":" + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return tool
}

// A Decision can redirect a call, replace its input and leave a note, and the
// result event names what actually ran.
func TestADecisionRedirectsEditsAndNotes(t *testing.T) {
	search, draft := echoInput(t, "search"), echoInput(t, "draft")
	sink := &nacelle.ToolSink{
		Tools: nacelle.ToolsByName([]nacelle.Tool{search, draft}),
		Approve: func(_ context.Context, call nacelle.Call) nacelle.Decision {
			return nacelle.Decision{Name: "draft", Input: json.RawMessage(`{"query":"edited"}`), Note: "edited by the user"}
		},
	}
	result, err := nacelle.RunTool(context.Background(), search, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	if err != nil || result != "edited by the user\n\ndraft:edited" {
		t.Fatalf("RunTool = %q, %v", result, err)
	}
	events := sink.Drain()
	if len(events) != 1 || events[0].Tool.Name != "draft" || events[0].Tool.Input != `{"query":"edited"}` {
		t.Fatalf("events = %+v, want one result naming the redirected call", events)
	}
}

// A redirect to a tool nobody registered refuses the call rather than running
// the original one.
func TestARedirectToAnUnknownToolIsRefused(t *testing.T) {
	sink := &nacelle.ToolSink{Approve: func(context.Context, nacelle.Call) nacelle.Decision {
		return nacelle.Decision{Name: "missing"}
	}}
	_, err := nacelle.RunTool(context.Background(), echoInput(t, "search"), nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	if err == nil || !sink.Drain()[0].Tool.Refused {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

// A BeforeToolCall hook that returns Input rewrites the call for Approve and
// for the tool.
func TestABeforeHookRewritesTheInput(t *testing.T) {
	var approved string
	sink := &nacelle.ToolSink{
		Hooks: map[nacelle.HookPoint][]nacelle.Hook{nacelle.BeforeToolCall: {
			func(context.Context, nacelle.HookEvent) nacelle.HookResult {
				return nacelle.HookResult{Input: json.RawMessage(`{"query":"hooked"}`)}
			},
		}},
		Approve: func(_ context.Context, call nacelle.Call) nacelle.Decision {
			approved = string(call.Input)
			return nacelle.Decision{}
		},
	}
	result, err := nacelle.RunTool(context.Background(), echoInput(t, "search"), nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	if err != nil || result != "search:hooked" || approved != `{"query":"hooked"}` {
		t.Fatalf("RunTool = %q, %v; Approve saw %s", result, err, approved)
	}
}

// EndTurn is recorded as a success carrying its result, and marks the sink as
// yielded so the backend stops after the round.
func TestEndTurnIsASuccessThatYields(t *testing.T) {
	tool, err := nacelle.NewTool("ask", "Ask", func(context.Context, struct{}) (string, error) {
		return "", nacelle.EndTurn("asked the user")
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	sink := &nacelle.ToolSink{}
	result, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, nil, sink)
	if err != nil || result != "asked the user" || !sink.Yielded() {
		t.Fatalf("RunTool = %q, %v, yielded %v", result, err, sink.Yielded())
	}
}

// ApproveBool turns a refusal into a Deny the model can read.
func TestApproveBoolDenies(t *testing.T) {
	approve := nacelle.ApproveBool(func(context.Context, string, json.RawMessage) bool { return false })
	sink := &nacelle.ToolSink{Approve: approve}
	_, err := nacelle.RunTool(context.Background(), echoInput(t, "search"), nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	if err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("err = %v, want a not-approved refusal", err)
	}
}
