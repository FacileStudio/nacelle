package nacelle_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"testing"

	"github.com/FacileStudio/nacelle"
)

func collectChannel(calls <-chan string, count int) map[string]int {
	got := make(map[string]int)
	for timeout := 0; timeout < 100; timeout++ {
		select {
		case c := <-calls:
			got[c] = 1
		default:
		}
		if len(got) == count {
			break
		}
	}
	return got
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
	}, nacelle.ParallelSubAgentOptions{Callbacks: nacelle.ParallelSubAgentCallbacks{Tool: func(batch string, idx int, name string) {
		calls <- fmt.Sprintf("%s:%d:%s", batch, idx, name)
	}}})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["t0","t1"]}`), sink)
	drainToolResult(t, sink)

	got := collectChannel(calls, 2)
	for _, want := range []string{":0:echo", ":1:echo"} {
		if got[want] == 0 {
			t.Errorf("tool calls = %v, want %q reported", got, want)
		}
	}
}
// TestParallelSubAgentReportsToolDone verifies the ToolDone callback fires as a
// nested task's tool call ends with err nil on success, so a host can colour the
// running-tool glyph green the moment its call lands. Echo needs an approving
// policy, because the nested agent's default deny-all refuses every call. 
func TestParallelSubAgentReportsToolDone(t *testing.T) {
	backend := newLoop([]step{toolStep("echo", `{}`), textStep("ok")})
	echo := &echoTool{}
	done := make(chan string, 4)

	cb := func(batch string, idx int, name string, err error) {
		s := "ok"
		if err != nil {
			s = "fail"
		}
		done <- fmt.Sprintf("%d:%s:%s", idx, name, s)
	}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{
		Approve:   func(context.Context, string, json.RawMessage) bool { return true },
		Callbacks: nacelle.ParallelSubAgentCallbacks{ToolDone: cb},
	})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["t0"]}`), sink)
	drainToolResult(t, sink)

	if !awaitKey(t, done, "0:echo:ok") {
		t.Error("tool done did not report echo succeeded")
	}
}
// TestParallelSubAgentReportsToolFailure verifies the ToolDone callback fires
// with a set err when a nested tool's call fails. The "nope" tool is not in the
// nested agent's tool list, so the loop backend reports its call as an error
// without approval being involved. 
func TestParallelSubAgentReportsToolFailure(t *testing.T) {
	backend := newLoop([]step{toolStep("nope", `{}`), textStep("ok")})
	echo := &echoTool{}
	done := make(chan string, 4)

	cb := func(batch string, idx int, name string, err error) {
		s := "ok"
		if err != nil {
			s = "fail"
		}
		done <- fmt.Sprintf("%d:%s:%s", idx, name, s)
	}

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: backend, System: "s", Tools: []nacelle.Tool{echo},
	}, nacelle.ParallelSubAgentOptions{Callbacks: nacelle.ParallelSubAgentCallbacks{ToolDone: cb}})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["t0"]}`), sink)
	drainToolResult(t, sink)

	if !awaitKey(t, done, "0:nope:fail") {
		t.Error("tool done did not report the unknown tool failing")
	}
}

// awaitKey drains a channel until a wanted value appears, or the poll budget
// runs out. It returns whether the value was seen.
func awaitKey(t *testing.T, c <-chan string, want string) bool {
	var got []string
	for timeout := 0; timeout < 200; timeout++ {
		select {
		case v := <-c:
			if v == want {
				return true
			}
			got = append(got, v)
		default:
		}
	}
	t.Errorf("channel held %v, want %q", got, want)
	return false
}

// turnBackend streams a single turn carrying fixed usage, so a host can assert
// what the parallel tool forwards per task as each nested run spends.
type turnBackend struct{}

func (turnBackend) Name() string                       { return "turn" }
func (turnBackend) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (turnBackend) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}
func (turnBackend) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		if !yield(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{InputTokens: 10, OutputTokens: 2}}, nil) {
			return
		}
		yield(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopEnd}, nil)
	}
}
// TestParallelSubAgentReportsLiveUsage verifies the LiveUsage callback fires as
// each nested task's turn spends, tagged with the task's index, so a host can
// draw a per-subagent token counter that moves while the fan-out runs. 
func TestParallelSubAgentReportsLiveUsage(t *testing.T) {
	reports := make(chan string, 6)

	sub, err := nacelle.NewParallelSubAgentTool(nacelle.Config{
		Backend: turnBackend{}, System: "s",
	}, nacelle.ParallelSubAgentOptions{Callbacks: nacelle.ParallelSubAgentCallbacks{LiveUsage: func(batch string, idx int, usage nacelle.Usage) {
		reports <- fmt.Sprintf("%s:%d:%d:%d", batch, idx, usage.InputTokens, usage.OutputTokens)
	}}})
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), sub, nacelle.Invocation{ID: "x"},
		json.RawMessage(`{"tasks":["t0","t1"]}`), sink)
	drainToolResult(t, sink)

	got := collectChannel(reports, 2)
	for _, want := range []string{":0:10:2", ":1:10:2"} {
		if got[want] == 0 {
			t.Errorf("live usage = %v, want %q reported", got, want)
		}
	}
}
