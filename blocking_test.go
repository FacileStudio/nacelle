package nacelle_test

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
)

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
	tool, err := nacelle.NewParallelSubAgentTool(
		nacelle.Config{Backend: &blocking{}, System: "outer"},
		nacelle.ParallelSubAgentOptions{},
	)
	if err != nil {
		t.Fatalf("NewParallelSubAgentTool: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := &nacelle.ToolSink{}
	done := make(chan struct{})
	go func() {
		nacelle.RunTool(ctx, tool, nacelle.Invocation{ID: "x"}, json.RawMessage(`{"tasks":["work"]}`), sink)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the delegation ignored the cancelled context")
	}
	events := sink.Drain()
	if len(events) == 0 || events[len(events)-1].Tool == nil {
		t.Fatalf("events = %v, want a tool result carrying the cancellation", events)
	}
	if !strings.Contains(events[len(events)-1].Tool.Result, "context canceled") {
		t.Fatalf("result = %q, want the cancellation surfaced as the task error", events[len(events)-1].Tool.Result)
	}
}
