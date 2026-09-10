package nacelle_test

import (
	"context"
	"encoding/json"
	"iter"
	"strconv"
	"testing"

	"github.com/FacileStudio/nacelle"
)

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
func TestParallelSubAgentReportsTurnUsage(t *testing.T) {
	var seen []nacelle.Usage
	tool, err := nacelle.NewParallelSubAgentTool(
		nacelle.Config{Backend: &turns{spent: nacelle.Usage{InputTokens: 10, OutputTokens: 5}}, System: "outer"},
		nacelle.ParallelSubAgentOptions{Usage: func(u nacelle.Usage) { seen = append(seen, u) }},
	)
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}
	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "x"}, json.RawMessage(`{"tasks":["work"]}`), sink)
	for _, event := range sink.Drain() {
		if event.Tool != nil && event.Tool.Err != nil {
			t.Fatalf("delegation failed: %v", event.Tool.Err)
		}
	}
	if len(seen) != 1 || seen[0].OutputTokens != 5 {
		t.Fatalf("usage = %v, want the one turn's spend", seen)
	}
}

// The parallel result carries each task's spend in a per-index usage map, so a
// consumer can bill a single subagent without summing the whole fan-out.
func TestParallelSubAgentResponseCarriesPerTaskUsage(t *testing.T) {
	tool, err := nacelle.NewParallelSubAgentTool(
		nacelle.Config{Backend: &turns{spent: nacelle.Usage{InputTokens: 10, OutputTokens: 5}}, System: "outer"},
		nacelle.ParallelSubAgentOptions{},
	)
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}
	sink := &nacelle.ToolSink{}
	nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "x"}, json.RawMessage(`{"tasks":["one","two"]}`), sink)

	var result string
	for _, event := range sink.Drain() {
		if event.Tool != nil && event.Tool.Err != nil {
			t.Fatalf("delegation failed: %v", event.Tool.Err)
		}
		if event.Tool != nil && event.Tool.Result != "" {
			result = event.Tool.Result
		}
	}

	var resp parallelUsageResult
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	checkUsage(t, resp, 0, 10)
	checkUsage(t, resp, 1, 10)
}

type parallelUsageResult struct {
	Tasks  map[string]string        `json:"tasks"`
	Errors map[string]string        `json:"errors,omitempty"`
	Usage  map[string]nacelle.Usage `json:"usage,omitempty"`
}

func checkUsage(t *testing.T, resp parallelUsageResult, idx int, wantInput int64) {
	t.Helper()
	key := strconv.Itoa(idx)
	if u, ok := resp.Usage[key]; !ok {
		if e, ok := resp.Errors[key]; ok {
			t.Fatalf("task %s errored: %v", key, e)
		}
		t.Fatalf("usage missing for task %s", key)
	} else if u.InputTokens != wantInput {
		t.Errorf("task %s InputTokens = %d, want %d", key, u.InputTokens, wantInput)
	}
}
