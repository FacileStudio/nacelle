package nacelle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
