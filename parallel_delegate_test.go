package nacelle_test

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)
// TestParallelSubAgentMaxConcurrencyClamped verifies that maxConcurrency
// values above 32 are clamped to 32.
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

type parallelConcurrencyTracker struct {
	active    atomic.Int32
	maxActive atomic.Int32
	release   chan struct{}
}

func (b *parallelConcurrencyTracker) Name() string { return "parallelConcurrencyTracker" }
func (b *parallelConcurrencyTracker) Capabilities() nacelle.Capabilities {
	return nacelle.Capabilities{}
}
func (b *parallelConcurrencyTracker) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}
func (b *parallelConcurrencyTracker) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		cur := b.active.Add(1)
		for {
			max := b.maxActive.Load()
			if cur <= max || b.maxActive.CompareAndSwap(max, cur) {
				break
			}
		}
		<-b.release
		b.active.Add(-1)
		yield(nacelle.Event{Kind: nacelle.KindText, Text: "ok"}, nil)
		yield(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopEnd}, nil)
	}
}// TestParallelSubAgentConcurrentFanOut verifies that 8 or more agents
// run concurrently.
func TestParallelSubAgentConcurrentFanOut(t *testing.T) {
	backend := &parallelConcurrencyTracker{release: make(chan struct{})}
	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s",
	}, nacelle.ParallelSubAgentOptions{MaxConcurrency: 16})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	go func() {
		for backend.active.Load() < 8 {
			time.Sleep(time.Millisecond)
		}
		close(backend.release)
	}()

	sink := &nacelle.ToolSink{}
	tasks := `{"tasks":["t1","t2","t3","t4","t5","t6","t7","t8","t9","t10"]}`
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"}, json.RawMessage(tasks), sink)

	var resp struct {
		Tasks map[string]string `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(drainToolResult(t, sink)), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(resp.Tasks) != 10 || backend.maxActive.Load() < 8 {
		t.Errorf("got %d tasks, max active %d", len(resp.Tasks), backend.maxActive.Load())
	}
}// TestParallelSubAgentConcurrency verifies that the concurrency semaphore
// actually limits how many tasks run simultaneously. It uses a backend that
// records the wall time of each call and checks that no two calls overlap.
func TestParallelSubAgentConcurrency(t *testing.T) {
	backend := &concurrencyTracker{}
	echo := &echoTool{}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{MaxConcurrency: 8})
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
}// TestDelegateParallelStreamsResults verifies the detached surface posts one
// result per task through the channel, keyed by the task's index so the caller
// can reunite them with the list it gave regardless of completion order. 

func drainParallelResults(ch <-chan nacelle.ParallelTaskResult, t *testing.T) map[int]string {
	t.Helper()
	got := make(map[int]string)
	for next := range ch {
		if next.Err != "" {
			t.Errorf("task %d failed: %q", next.Index, next.Err)
			continue
		}
		got[next.Index] = next.Result
	}
	return got
}
func TestDelegateParallelStreamsResults(t *testing.T) {
	results, err := nacelle.DelegateParallel(context.Background(), nacelle.Config{
		Backend: newLoop(
			[]step{toolStep("echo", `{}`), textStep("result1")},
			[]step{toolStep("echo", `{}`), textStep("result2")},
			[]step{toolStep("echo", `{}`), textStep("result3")},
		), System: "s", Tools: []nacelle.Tool{&echoTool{}},
	}, []string{"task1", "task2", "task3"}, nacelle.ParallelSubAgentOptions{})
	if err != nil {
		t.Fatalf("DelegateParallel: %v", err)
	}

	got := drainParallelResults(results, t)
	if len(got) != 3 {
		t.Errorf("got %d results, want 3", len(got))
	}
	joined := got[0] + " " + got[1] + " " + got[2]
	for _, want := range []string{"result1", "result2", "result3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("result set %q missing %q", joined, want)
		}
	}
}// TestDelegateParallelEmptyTasksErrors verifies the detached surface rejects an
// empty task list, matching the tool's own failure.
func TestDelegateParallelEmptyTasksErrors(t *testing.T) {
	_, err := nacelle.DelegateParallel(context.Background(), nacelle.Config{
		Backend: newLoop(), System: "s",
	}, []string{}, nacelle.ParallelSubAgentOptions{})
	if err == nil {
		t.Error("DelegateParallel accepted an empty task list")
	}
}