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
	request := nacelle.Request{
		Tools: []nacelle.Tool{search, draft},
		Approve: func(_ context.Context, call nacelle.Call) nacelle.Decision {
			return nacelle.Decision{Name: "draft", Input: json.RawMessage(`{"query":"edited"}`), Note: "edited by the user"}
		},
	}
	got := runTool(t, context.Background(), request, search, `{"query":"x"}`)
	if got.err != nil || got.result != "edited by the user\n\ndraft:edited" {
		t.Fatalf("ran = %q, %v", got.result, got.err)
	}
	events := got.toolEvents()
	if len(events) != 1 || events[0].Tool.Name != "draft" || events[0].Tool.Input != `{"query":"edited"}` {
		t.Fatalf("events = %+v, want one result naming the redirected call", events)
	}
}

// A redirect to a tool nobody registered refuses the call rather than running
// the original one.
func TestARedirectToAnUnknownToolIsRefused(t *testing.T) {
	request := nacelle.Request{Approve: func(context.Context, nacelle.Call) nacelle.Decision {
		return nacelle.Decision{Name: "missing"}
	}}
	got := runTool(t, context.Background(), request, echoInput(t, "search"), `{"query":"x"}`)
	if got.err == nil || !got.tool.Refused {
		t.Fatalf("err = %v, want a refusal", got.err)
	}
}

// A BeforeToolCall hook that returns Input rewrites the call for Approve and
// for the tool.
func TestABeforeHookRewritesTheInput(t *testing.T) {
	var approved string
	rewrite := func(context.Context, nacelle.HookEvent) nacelle.HookResult {
		return nacelle.HookResult{Input: json.RawMessage(`{"query":"hooked"}`)}
	}
	request := nacelle.Request{
		Hooks: map[nacelle.HookPoint][]nacelle.Hook{nacelle.BeforeToolCall: {rewrite}},
		Approve: func(_ context.Context, call nacelle.Call) nacelle.Decision {
			approved = string(call.Input)
			return nacelle.Decision{}
		},
	}
	got := runTool(t, context.Background(), request, echoInput(t, "search"), `{"query":"x"}`)
	if got.err != nil || got.result != "search:hooked" || approved != `{"query":"hooked"}` {
		t.Fatalf("ran = %q, %v; Approve saw %s", got.result, got.err, approved)
	}
}

// EndTurn is recorded as a success carrying its result, and ends the run with
// StopYield after the round.
func TestEndTurnIsASuccessThatYields(t *testing.T) {
	tool, err := nacelle.NewTool("ask", "Ask", func(context.Context, struct{}) (string, error) {
		return "", nacelle.EndTurn("asked the user")
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	got := runTool(t, context.Background(), nacelle.Request{}, tool, ``)
	if got.err != nil || got.result != "asked the user" || got.stop != nacelle.StopYield {
		t.Fatalf("ran = %q, %v, stop %q", got.result, got.err, got.stop)
	}
}

// ApproveBool turns a refusal into a Deny the model can read.
func TestApproveBoolDenies(t *testing.T) {
	approve := nacelle.ApproveBool(func(context.Context, string, json.RawMessage) bool { return false })
	got := runTool(t, context.Background(), nacelle.Request{Approve: approve}, echoInput(t, "search"), `{"query":"x"}`)
	if got.err == nil || !strings.Contains(got.err.Error(), "not approved") {
		t.Fatalf("err = %v, want a not-approved refusal", got.err)
	}
}
