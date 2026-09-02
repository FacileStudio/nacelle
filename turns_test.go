package nacelle_test

import (
	"context"
	"encoding/json"
	"iter"
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
