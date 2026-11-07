package nacelle_test

import (
	"context"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

func awaitAlive(alive <-chan bool, count int, t *testing.T) []bool {
	t.Helper()
	var seen []bool
	deadline := time.After(5 * time.Second)
	for len(seen) < count {
		select {
		case ok := <-alive:
			seen = append(seen, ok)
		case <-deadline:
			t.Fatalf("timed out waiting for the subagents to stream")
		}
	}
	return seen
}

func verifyCancelStub(t *testing.T, sink ran, runErr error) {
	t.Helper()
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
}

func awaitParallelResults(got <-chan nacelle.ParallelTaskResult, count int, t *testing.T) []string {
	t.Helper()
	results := make([]string, 0, count)
	deadline := time.After(5 * time.Second)
	for len(results) < count {
		select {
		case r := <-got:
			results = append(results, r.Result)
		case <-deadline:
			t.Fatalf("timed out waiting for streamed results, got %v", results)
		}
	}
	return results
}

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
	}, nacelle.ParallelSubAgentOptions{Detach: true, Callbacks: nacelle.ParallelSubAgentCallbacks{Results: func(r nacelle.ParallelTaskResult) { got <- r }}})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := runTool(t, context.Background(), nacelle.Request{}, sub, `{"tasks":["task1","task2","task3"]}`)
	runErr := sink.err

	verifyCancelStub(t, sink, runErr)

	results := awaitParallelResults(got, 3, t)
	if !strings.Contains(strings.Join(results, " "), "result1") ||
		!strings.Contains(strings.Join(results, " "), "result2") ||
		!strings.Contains(strings.Join(results, " "), "result3") {
		t.Errorf("streamed results = %v, want result1/result2/result3", results)
	}
}

// ctxRecord is a backend whose streams wait for release, then report whether
// their context is still alive and yield one finished run. The test releases
// them only after cancelling the caller, so a stream that shares the caller's
// context reports dead, while the fan-out's own context cannot have ended yet:
// it waits for these very streams to finish.
type ctxRecord struct {
	release chan struct{}
	alive   chan bool
}

func (b *ctxRecord) Name() string                                                { return "ctxrecord" }
func (b *ctxRecord) Capabilities() nacelle.Capabilities                          { return nacelle.Capabilities{} }
func (b *ctxRecord) CountTokens(context.Context, nacelle.Request) (int64, error) { return 0, nil }

func (b *ctxRecord) Stream(ctx context.Context, _ nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		<-b.release
		b.alive <- ctx.Err() == nil
		if yield(nacelle.Event{Kind: nacelle.KindText, Text: "ok"}, nil) {
			yield(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopEnd}, nil)
		}
	}
}

// TestParallelSubAgentDetachSurvivesParentCancel verifies that cancelling the
// caller's context does not take a detached fan-out down with it. The parent
// that launched the subagents sent its turn back to ready — the running agent
// settles, which cancels the turn's context — and the work it left grinding
// in the background has to keep going. A detached fan-out must run on its own
// background context, never the caller's cancellable one.
func TestParallelSubAgentDetachSurvivesParentCancel(t *testing.T) {
	backend := &ctxRecord{release: make(chan struct{}), alive: make(chan bool, 3)}
	got := make(chan nacelle.ParallelTaskResult, 3)

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s",
	}, nacelle.ParallelSubAgentOptions{Detach: true, Callbacks: nacelle.ParallelSubAgentCallbacks{Results: func(r nacelle.ParallelTaskResult) { got <- r }}})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sink := runTool(t, ctx, nacelle.Request{}, sub, `{"tasks":["task1","task2","task3"]}`)

	stub := drainToolResult(t, sink)
	if !strings.Contains(stub, `"started":3`) {
		t.Fatalf("stub %q does not say 3 agents started", stub)
	}

	cancel()
	close(backend.release)

	for _, ok := range awaitAlive(backend.alive, 3, t) {
		if !ok {
			t.Error("a detached subagent streamed on a cancelled context — the fan-out must outlive the parent's turn")
		}
	}

	results := awaitParallelResults(got, 3, t)
	if len(results) != 3 {
		t.Errorf("got %d streamed results, want 3", len(results))
	}
}
