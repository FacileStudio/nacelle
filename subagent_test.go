package nacelle_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// loop is a backend that plays the agent loop itself: each Stream call is
// one agent run, and it walks its scripted steps in order, running tools
// through RunTool exactly as a real backend would until a step answers. It
// is how delegation is tested end to end without a network — the first run
// belongs to the parent and asks for the subagent tool; the second is the
// nested agent on this same backend, answering its own script.
type loop struct {
	mu       sync.Mutex
	runs     [][]step
	requests []nacelle.Request
	next     int
}

func (l *loop) Name() string                       { return "loop" }
func (l *loop) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (l *loop) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}

// step is one model turn: either asking for a tool — answer carries the raw
// input the tool gets — or giving one as text, with the stop reason a real
// response would have ended on.
type step struct {
	tool   string
	input  string
	answer string
	stop   nacelle.Stop
}

func toolStep(tool, input string) step { return step{tool: tool, input: input} }

func textStep(answer string, stop ...nacelle.Stop) step {
	s := step{answer: answer}
	if len(stop) > 0 {
		s.stop = stop[0]
	}
	return s
}

func newLoop(runs ...[]step) *loop { return &loop{runs: runs} }

func (l *loop) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	l.mu.Lock()
	index := l.next
	l.next++
	l.requests = append(l.requests, request)
	steps := []step{}
	if index < len(l.runs) {
		steps = l.runs[index]
	}
	sink := &nacelle.ToolSink{Approve: request.Approve}
	byName := nacelle.ToolsByName(request.Tools)
	l.mu.Unlock()

	return func(yield func(nacelle.Event, error) bool) {
		for _, s := range steps {
			if s.tool == "" {
				stop := s.stop
				if stop == "" {
					stop = nacelle.StopEnd
				}
				if !yield(nacelle.Event{Kind: nacelle.KindText, Text: s.answer}, nil) {
					return
				}
				yield(nacelle.Event{Kind: nacelle.KindDone, Stop: stop}, nil)
				return
			}
			id := fmt.Sprintf("call_%d_%s", index, s.tool)
			if !yield(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{
				ID: id, Name: s.tool, Input: s.input,
			}}, nil) {
				return
			}
			if tool := byName[s.tool]; tool != nil {
				nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: id}, json.RawMessage(s.input), sink)
			} else {
				sink.Report(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{
					ID: id, Name: s.tool, Input: s.input,
					Err: fmt.Errorf("no such tool %q", s.tool),
				}})
			}
			for _, drained := range sink.Drain() {
				if !yield(drained, nil) {
					return
				}
			}
		}
	}
}

// echoTool records every real run, so a test can tell an approved call from a
// refused one without parsing results back.
type echoTool struct {
	mu  sync.Mutex
	ran []string
}

func (e *echoTool) Name() string { return "echo" }
func (e *echoTool) Description() string {
	return "repeat the task back"
}
func (e *echoTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}

func (e *echoTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ran = append(e.ran, string(input))
	return "echo ran", nil
}

func (e *echoTool) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.ran)
}

// A delegation runs end to end: the parent's model asks for the subagent
// tool, the nested agent works its own script with its own message list, and
// only the delegate's final text reaches the parent's model.
func TestSubAgentDelegatesAndReturnsTheFinalAnswer(t *testing.T) {
	// Run one is the parent delegating then answering; run two is the
	// nested agent, whose two echo attempts are refused and which then
	// gives up with text.
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

	var final strings.Builder
	for event, err := range parent.Stream(context.Background(), []nacelle.Message{{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.Text{Text: "go"}}}}) {
		if err != nil {
			t.Fatalf("parent stream: %v", err)
		}
		if event.Kind == nacelle.KindText {
			final.WriteString(event.Text)
		}
	}
	if got := final.String(); got != "the parent heard: seven" {
		t.Errorf("final text = %q, want the parent's own answer", got)
	}
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

// blocking is a backend whose stream never yields: it waits on its channel,
// so a delegation against it only ends when the context does.
type blocking struct{}

func (b *blocking) Name() string                       { return "blocking" }
func (b *blocking) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (b *blocking) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}
func (b *blocking) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		<-ctx.Done()
		yield(nacelle.Event{}, ctx.Err())
	}
}

// A cancelled context ends the delegation instead of leaving the nested run
// running behind a tool result nobody will read — esc in the parent's UI has
// to reach the delegate, because a delegation is minutes of billed work.
func TestDelegateHonoursACancelledContext(t *testing.T) {
	tool, err := nacelle.NewSubAgentTool(
		nacelle.Config{Backend: &blocking{}, System: "outer"},
		nacelle.SubAgentOptions{},
	)
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := &nacelle.ToolSink{}
	done := make(chan struct{})
	go func() {
		nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: "x"}, json.RawMessage(`{"task":"work"}`), sink)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the delegation ignored the cancelled context")
	}
	events := sink.Drain()
	if len(events) == 0 || events[len(events)-1].Tool == nil || events[len(events)-1].Tool.Err == nil {
		t.Fatalf("events = %v, want a tool result carrying the cancellation", events)
	}
}

// turns is a backend that answers once and bills the turn, so the Usage
// hook has something to carry.
type turns struct{ spent nacelle.Usage }

func (t *turns) Name() string                       { return "turns" }
func (t *turns) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (t *turns) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}
func (t *turns) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		yield(nacelle.Event{Kind: nacelle.KindText, Text: "answer"}, nil)
		if !yield(nacelle.Event{Kind: nacelle.KindTurn, Usage: t.spent}, nil) {
			return
		}
		yield(nacelle.Event{Kind: nacelle.KindDone}, nil)
	}
}

// The Usage hook sees every nested turn's cost as it is spent.
func TestDelegateReportsTurnUsage(t *testing.T) {
	var seen []nacelle.Usage
	tool, err := nacelle.NewSubAgentTool(
		nacelle.Config{Backend: &turns{spent: nacelle.Usage{InputTokens: 10, OutputTokens: 5}}, System: "outer"},
		nacelle.SubAgentOptions{Usage: func(u nacelle.Usage) { seen = append(seen, u) }},
	)
	if err != nil {
		t.Fatalf("NewSubAgentTool: %v", err)
	}
	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "x"}, json.RawMessage(`{"task":"work"}`), sink)
	for _, event := range sink.Drain() {
		if event.Tool != nil && event.Tool.Err != nil {
			t.Fatalf("delegation failed: %v", event.Tool.Err)
		}
	}
	if len(seen) != 1 || seen[0].OutputTokens != 5 {
		t.Fatalf("usage = %v, want the one turn's spend", seen)
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
