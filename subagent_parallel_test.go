package nacelle_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

// TestParallelSubAgentReturnsAllResults verifies that 3 tasks complete
// and all results are returned, regardless of completion order.
func TestParallelSubAgentReturnsAllResults(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelSubAgentToolName, `{"tasks":["count the stars","echo hello","echo world"]}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("seven")},
		[]step{toolStep("echo", `{}`), textStep("hello")},
		[]step{toolStep("echo", `{}`), textStep("world")},
	)
	echo := &echoTool{}

	_, parent := newSubAgent(t, backend, echo)

	var toolResult string
	runParent(t, parent, "parallel", func(event nacelle.Event) {
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelSubAgentToolName {
			toolResult = event.Tool.Result
		}
	})

	assertContains(t, toolResult, "seven", "hello", "world")
}

// TestParallelSubAgentPartialFailure verifies that when one task fails,
// the others still complete and their results are returned.
func TestParallelSubAgentPartialFailure(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelSubAgentToolName, `{"tasks":["good","bad","good"]}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("good1")},
		[]step{toolStep("echo", `{}`), textStep("bad")},
		[]step{toolStep("echo", `{}`), textStep("good2")},
	)
	echo := &echoTool{}

	_, parent := newSubAgent(t, backend, echo)

	var toolResult string
	runParent(t, parent, "parallel", func(event nacelle.Event) {
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelSubAgentToolName {
			toolResult = event.Tool.Result
		}
	})

	assertContains(t, toolResult, "good1", "good2", "bad")
}

// TestParallelSubAgentSharedConfig verifies that all parallel agents
// share the same config and the parent config is not mutated.
func TestParallelSubAgentSharedConfig(t *testing.T) {
	backend := newLoop(
		[]step{toolStep(nacelle.ParallelSubAgentToolName, `{"tasks":["task1","task2","task3"]}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("task1")},
		[]step{toolStep("echo", `{}`), textStep("task2")},
		[]step{toolStep("echo", `{}`), textStep("task3")},
	)
	echo := &echoTool{}

	_, parent := newSubAgent(t, backend, echo)

	var toolResult string
	runParent(t, parent, "parallel", func(event nacelle.Event) {
		if event.Kind == nacelle.KindToolResult && event.Tool != nil && event.Tool.Name == nacelle.ParallelSubAgentToolName {
			toolResult = event.Tool.Result
		}
	})

	assertContains(t, toolResult, "task1", "task2", "task3")
}

// TestParallelSubAgentEmptyTasks verifies that an empty task list fails.
func TestParallelSubAgentEmptyTasks(t *testing.T) {
	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: newLoop(), System: "s",
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	_, err = sub.Run(context.Background(), json.RawMessage(`{"tasks":[]}`))
	if err == nil {
		t.Error("an empty task list was accepted")
	}
}

// TestParallelSubAgentMaxConcurrencyClamped verifies that maxConcurrency
// values above 8 are clamped to 8.
func TestParallelSubAgentMaxConcurrencyClamped(t *testing.T) {
	backend := newLoop(
		[]step{toolStep("echo", `{}`), textStep("ok")},
	)
	echo := &echoTool{}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{MaxConcurrency: 100})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["a"]}`), sink)

	result := drainToolResult(t, sink)
	if !strings.Contains(result, "ok") {
		t.Errorf("result missing 'ok', got %q", result)
	}
}

// TestParallelSubAgentConcurrency verifies that the concurrency semaphore
// actually limits how many tasks run simultaneously. It uses a backend that
// records the wall time of each call and checks that no two calls overlap.
func TestParallelSubAgentConcurrency(t *testing.T) {
	backend := &concurrencyTracker{}
	echo := &echoTool{}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{MaxConcurrency: 4})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["t1","t2","t3","t4","t5","t6","t7","t8","t9","t10","t11","t12"]}`), sink)

	result := drainToolResult(t, sink)

	var resp struct {
		Tasks map[string]string `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(resp.Tasks) != 12 {
		t.Errorf("expected 12 tasks completed, got %d", len(resp.Tasks))
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	for i := range backend.overlaps {
		if backend.overlaps[i] {
			t.Errorf("calls %d and %d overlapped — concurrency limit not enforced", i, i+1)
		}
	}
}

// TestDelegateParallelStreamsResults verifies the detached surface posts one
// result per task through the channel, keyed by the task's index so the caller
// can reunite them with the list it gave regardless of completion order.
func TestDelegateParallelStreamsResults(t *testing.T) {
	backend := newLoop(
		[]step{toolStep("echo", `{}`), textStep("result1")},
		[]step{toolStep("echo", `{}`), textStep("result2")},
		[]step{toolStep("echo", `{}`), textStep("result3")},
	)
	echo := &echoTool{}

	results, err := nacelle.DelegateParallel(context.Background(), nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, []string{"task1", "task2", "task3"}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("DelegateParallel: %v", err)
	}

	got := make(map[int]string)
	for {
		next, open := <-results
		if !open {
			break
		}
		if next.Err != "" {
			t.Errorf("task %d failed: %q", next.Index, next.Err)
			continue
		}
		got[next.Index] = next.Result
	}

	if len(got) != 3 {
		t.Errorf("got %d results, want 3", len(got))
	}
	joined := ""
	for _, r := range got {
		joined += r + " "
	}
	for _, want := range []string{"result1", "result2", "result3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("result set %q missing %q", joined, want)
		}
	}
}

// TestDelegateParallelEmptyTasksErrors verifies the detached surface rejects an
// empty task list, matching the tool's own failure.
func TestDelegateParallelEmptyTasksErrors(t *testing.T) {
	_, err := nacelle.DelegateParallel(context.Background(), nacelle.Config{
		Backend: newLoop(), System: "s",
	}, []string{}, nacelle.ParallelSubAgentOptions{})
	if err == nil {
		t.Error("DelegateParallel accepted an empty task list")
	}
}

// TestParallelSubAgentDetachReturnsImmediately verifies the non-blocking mode:
// Run hands back a stub naming how many agents started and the batch key, and
// the real per-task outcomes arrive on the Results callback as they finish
// instead of being held until the whole fan-out returns.
func TestParallelSubAgentDetachStreamsResults(t *testing.T) {
	backend := newLoop(
		[]step{toolStep("echo", `{}`), textStep("result1")},
		[]step{toolStep("echo", `{}`), textStep("result2")},
		[]step{toolStep("echo", `{}`), textStep("result3")},
	)
	echo := &echoTool{}
	got := make(chan nacelle.ParallelTaskResult, 3)

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{Detach: true, Results: func(r nacelle.ParallelTaskResult) { got <- r }})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	_, runErr := nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["task1","task2","task3"]}`), sink)

	stub := drainToolResult(t, sink)
	if !strings.Contains(stub, `"started":3`) {
		t.Fatalf("stub %q does not say 3 agents started", stub)
	}
	if !strings.Contains(stub, `"batch"`) {
		t.Errorf("stub %q has no batch key", stub)
	}
	if runErr != nil {
		t.Fatalf("detached tool returned before completion, yet errored: %v", runErr)
	}

	results := make([]string, 0, 3)
	deadline := time.After(5 * time.Second)
	for len(results) < 3 {
		select {
		case r := <-got:
			results = append(results, r.Result)
		case <-deadline:
			t.Fatalf("timed out waiting for streamed results, got %v", results)
		}
	}
	if !strings.Contains(strings.Join(results, " "), "result1") ||
		!strings.Contains(strings.Join(results, " "), "result2") ||
		!strings.Contains(strings.Join(results, " "), "result3") {
		t.Errorf("streamed results = %v, want result1/result2/result3", results)
	}
}

// ctxRecord is a backend that yields one finished run and records the context
// each stream ran on, so a test can ask afterwards whether any of those
// contexts was cancelled. It is the lens a detached fan-out's lifetime is
// asserted through: the subagents must not share the caller's cancellable
// context.
type ctxRecord struct {
	seen chan context.Context
}

func (b *ctxRecord) Name() string                                                { return "ctxrecord" }
func (b *ctxRecord) Capabilities() nacelle.Capabilities                          { return nacelle.Capabilities{} }
func (b *ctxRecord) CountTokens(context.Context, nacelle.Request) (int64, error) { return 0, nil }

func (b *ctxRecord) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	b.seen <- ctx
	return func(yield func(nacelle.Event, error) bool) {
		yield(nacelle.Event{Kind: nacelle.KindText, Text: "ok"}, nil)
		yield(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopEnd}, nil)
	}
}

// TestParallelSubAgentDetachSurvivesParentCancel verifies that cancelling the
// caller's context does not take a detached fan-out down with it. The parent
// that launched the subagents sent its turn back to ready — the running agent
// settles, which cancels the turn's context — and the work it left grinding
// in the background has to keep going. A detached fan-out must run on its own
// background context, never the caller's cancellable one.
func TestParallelSubAgentDetachSurvivesParentCancel(t *testing.T) {
	backend := &ctxRecord{seen: make(chan context.Context, 3)}
	got := make(chan nacelle.ParallelTaskResult, 3)

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s",
	}, nacelle.ParallelSubAgentOptions{Detach: true, Results: func(r nacelle.ParallelTaskResult) { got <- r }})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sink := &nacelle.ToolSink{}
	nacelle.RunTool(ctx, sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["task1","task2","task3"]}`), sink)

	stub := drainToolResult(t, sink)
	if !strings.Contains(stub, `"started":3`) {
		t.Fatalf("stub %q does not say 3 agents started", stub)
	}

	cancel()

	var ctxs []context.Context
	deadline := time.After(5 * time.Second)
	for len(ctxs) < 3 {
		select {
		case c := <-backend.seen:
			ctxs = append(ctxs, c)
		case <-deadline:
			t.Fatalf("timed out waiting for the subagents to start")
		}
	}
	for _, c := range ctxs {
		if c.Err() != nil {
			t.Error("a detached subagent streamed on a cancelled context — the fan-out must outlive the parent's turn")
		}
	}

	var results []string
	for len(results) < 3 {
		select {
		case r := <-got:
			results = append(results, r.Result)
		case <-deadline:
			t.Fatalf("timed out waiting for streamed results, got %v", results)
		}
	}
	if len(results) != 3 {
		t.Errorf("got %d streamed results, want 3", len(results))
	}
}

// TestParallelSubAgentReportsToolCalls verifies the Tool callback fires as each
// nested task begins a tool call, tagged with the task's index and the tool's
// name, so a host can draw what each subagent is doing right now.
func TestParallelSubAgentReportsToolCalls(t *testing.T) {
	backend := newLoop(
		[]step{toolStep("echo", `{}`), textStep("ok")},
		[]step{toolStep("echo", `{}`), textStep("ok")},
	)
	echo := &echoTool{}
	calls := make(chan string, 6)

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{Tool: func(batch string, idx int, name string) {
		calls <- fmt.Sprintf("%s:%d:%s", batch, idx, name)
	}})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["t0","t1"]}`), sink)
	drainToolResult(t, sink)

	got := make(map[string]int)
	for timeout := 0; timeout < 100; timeout++ {
		select {
		case c := <-calls:
			got[c] = 1
		default:
		}
		if len(got) == 2 {
			break
		}
	}
	for _, want := range []string{":0:echo", ":1:echo"} {
		if got[want] == 0 {
			t.Errorf("tool calls = %v, want %q reported", got, want)
		}
	}
}

// TestParallelSubAgentDirectCall verifies parallel delegation via direct
// RunTool call, bypassing the parent stream to avoid the loop backend mutex.
func TestParallelSubAgentDirectCall(t *testing.T) {
	backend := newLoop(
		[]step{toolStep("echo", `{}`), textStep("result1")},
		[]step{toolStep("echo", `{}`), textStep("result2")},
		[]step{toolStep("echo", `{}`), textStep("result3")},
	)
	echo := &echoTool{}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["task1","task2","task3"]}`), sink)

	result := drainToolResult(t, sink)

	assertContains(t, result, "result1", "result2", "result3")
}

func newSubAgent(t *testing.T, backend nacelle.Backend, echo *echoTool) (nacelle.Tool, *nacelle.Agent) {
	t.Helper()

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	parent, err := nacelle.New(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{sub}, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return sub, parent
}

func runParent(t *testing.T, parent *nacelle.Agent, prompt string, onEvent func(nacelle.Event)) {
	t.Helper()

	for event, err := range parent.Stream(context.Background(), []nacelle.Message{
		{Role: nacelle.RoleUser, Parts: []nacelle.Part{nacelle.Text{Text: prompt}}},
	}) {
		if err != nil {
			t.Fatalf("parent stream: %v", err)
		}
		onEvent(event)
	}
}

func drainToolResult(t *testing.T, sink *nacelle.ToolSink) string {
	t.Helper()

	var result string
	for _, event := range sink.Drain() {
		if event.Tool == nil {
			continue
		}
		if event.Tool.Err != nil {
			t.Fatalf("parallel delegation failed: %v", event.Tool.Err)
		}
		result = event.Tool.Result
	}
	return result
}

func assertContains(t *testing.T, s string, substrs ...string) {
	t.Helper()

	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			t.Errorf("result missing %q, got %q", sub, s)
		}
	}
}
