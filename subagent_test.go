package nacelle_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// TestSubAgentDelegatesAndReturnsTheFinalAnswer verifies that a parent run
// asking for the subagent tool delegates to a nested run, that the nested
// run's own script is refused under deny-by-default, and that only the
// delegate's final text reaches the parent's model.
func TestSubAgentDelegatesAndReturnsTheFinalAnswer(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.SubAgentToolName, `{"task":"count the stars"}`), textStep("the parent heard: seven")},
		[]step{toolStep("echo", `{}`), toolStep("echo", `{}`), textStep("seven")},
	)
	echo := &echoTool{}

	sub, err := nacelle.NewSubAgentTool(nacelle.Config{
		Backend: backend, System: "outer", Tools: []nacelle.Tool{echo},
	}, nacelle.SubAgentOptions{})
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}

	parent, err := nacelle.New(nacelle.Config{
		Backend: backend, System: "outer",
		Tools: []nacelle.Tool{echo, sub}, MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	final := drainText(t, parent.Stream(context.Background(), []nacelle.Message{
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.Text{Text: "go"}}},
	}))
	if final != "the parent heard: seven" {
		t.Errorf("final text = %q, want the parent's own answer", final)
	}
	assertNestedRun(t, backend, echo)
}

// assertNestedRun checks the shape of the request the nested agent received:
// it inherits the parent's system prompt and conversation, carries its own
// tools minus the subagent, and denies every call by default.
func assertNestedRun(t *testing.T, backend *loop, echo *echoTool) {
	t.Helper()
	if len(backend.requests) != 2 {
		t.Fatalf("%d runs recorded, want one parent and one nested", len(backend.requests))
	}
	if echo.calls() != 0 {
		t.Errorf("the nested run ran echo %d times; deny-by-default should have refused every call", echo.calls())
	}

	inner := backend.requests[1]
	if inner.System != "outer" || len(inner.Messages) == 0 || inner.Messages[0].Parts[0].(nacelle.Text).Text != "count the stars" {
		t.Fatalf("nested request does not carry its own conversation: system %q", inner.System)
	}
	if slices.ContainsFunc(inner.Tools, func(tool nacelle.Tool) bool { return tool.Name() == nacelle.SubAgentToolName }) {
		t.Error("the nested run still carries the subagent tool; depth is unbounded")
	}
	if inner.Approve == nil {
		t.Fatal("the nested run has no approver at all; nil means every call runs unasked")
	}
	if inner.Approve(context.Background(), "echo", json.RawMessage(`{}`)) {
		t.Error("the default nested approver allowed a call; it must deny by default")
	}
}

// An opts.Approve that decides without asking lets the delegate actually use
// its tools, and the parent's own gate never sees inside the nested run.
func TestSubAgentHonoursASuppliedApprover(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.SubAgentToolName, `{"task":"use echo"}`), textStep("delegated")},
		[]step{toolStep("echo", `{"loud":true}`), textStep("done")},
	)
	echo := &echoTool{}

	sub, err := nacelle.NewSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.SubAgentOptions{Approve: func(context.Context, string, json.RawMessage) bool { return true }})
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}

	parent, err := nacelle.New(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{sub}, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, err := range parent.Stream(context.Background(), []nacelle.Message{{Role: nacelle.RoleUser}}) {
		if err != nil {
			t.Fatalf("parent stream: %v", err)
		}
	}
	if echo.calls() != 1 {
		t.Errorf("echo ran %d times, want once through the supplied approver", echo.calls())
	}
}

// A delegate that stops short says so rather than handing back a truncated
// answer shaped like a whole one.
func TestSubAgentReportsAnUnfinishedRun(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.SubAgentToolName, `{"task":"task"}`), textStep("ok")},
		[]step{textStep("half a thought", nacelle.StopMaxTokens)},
	)

	sub, err := nacelle.NewSubAgentTool(nacelle.Config{Backend: backend, System: "s"}, nacelle.SubAgentOptions{})
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}
	parent, err := nacelle.New(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{sub}, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var last nacelle.ToolEvent
	for event, err := range parent.Stream(context.Background(), []nacelle.Message{{Role: nacelle.RoleUser}}) {
		if err != nil {
			t.Fatalf("parent stream: %v", err)
		}
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.SubAgentToolName {
			last = *event.Tool
		}
	}
	if !strings.Contains(last.Result, "half a thought") || !strings.Contains(last.Result, "max_tokens") {
		t.Errorf("result = %q, want the partial answer plus the stop reason", last.Result)
	}
}

// An empty task fails the call rather than spending a run on nothing.
func TestSubAgentRefusesAnEmptyTask(t *testing.T) {
	sub, err := nacelle.NewSubAgentTool(nacelle.Config{Backend: newLoop(), System: "s"}, nacelle.SubAgentOptions{})
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}
	if _, err := sub.Run(context.Background(), json.RawMessage(`{"task":"   "}`)); err == nil {
		t.Error("an empty task was accepted")
	}
}

// A delegation retried through the same Retry wrapper another agent is
// using stays correct. The TUI builds exactly this shape — one wrapped
// backend handed to both the parent config and NewSubAgentTool — and the
// wrapper keeps its attempt count per Stream call, so the delegate's
// transient failure retries there and lands as a tool result rather than
// surfacing as the caller's error.
func TestDelegationRetriesThroughASharedRetryWrapper(t *testing.T) {
	backend := &flaky{runs: []run{
		{err: nacelle.Transient(errors.New("overloaded"))},
		{events: []nacelle.Event{{Kind: nacelle.KindText, Text: "seven"}, done}},
	}}
	wrapped := nacelle.Retry(backend, impatient())

	sub, err := nacelle.NewSubAgentTool(
		nacelle.Config{Backend: wrapped, System: "outer", MaxIterations: 5},
		nacelle.SubAgentOptions{},
	)
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}
	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"}, json.RawMessage(`{"task":"count the stars"}`), sink)

	var answer string
	for _, event := range sink.Drain() {
		if event.Tool == nil {
			continue
		}
		if event.Tool.Err != nil {
			t.Fatalf("delegation failed: %v", event.Tool.Err)
		}
		answer = event.Tool.Result
	}
	if answer != "seven" {
		t.Errorf("answer = %q, want the retried delegation's own text", answer)
	}
	if backend.calls != 2 {
		t.Errorf("calls = %d, want the failed attempt retried once", backend.calls)
	}
}
