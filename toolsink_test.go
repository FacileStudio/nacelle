package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// finished builds the event a backend parks in the sink for one tool that ran.
func finished(index int, name string) Event {
	return Event{Kind: KindToolResult, Tool: &ToolEvent{Index: index, Name: name}}
}

// outputTool is a Tool that also implements OutputTool, emitting each fragment
// it is handed and returning a fixed result.
type outputTool struct{}

func (outputTool) Name() string           { return "outputter" }
func (outputTool) Description() string    { return "an output-streaming tool" }
func (outputTool) Schema() map[string]any { return nil }
func (outputTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "done", nil
}
func (outputTool) RunOutput(_ context.Context, _ json.RawMessage, emit func(string)) (string, error) {
	emit("one")
	emit("two")
	return "done", nil
}

// A tool that implements OutputTool reports each emitted fragment as a
// KindToolOutput event ahead of its KindToolResult, so a consumer that drains
// the sink while the tool runs sees progress rather than only the finale.
func TestACallStreamsOutputBeforeTheResult(t *testing.T) {
	sink := newSink(Request{Tools: []Tool{outputTool{}}})
	result := sink.call(context.Background(), Call{ID: "c", Name: "outputter", Index: 2, Input: json.RawMessage(`{}`)})
	if result.Failed || result.Result != "done" {
		t.Fatalf("call = %+v; want the complete result", result)
	}

	events := sink.drain()
	if len(events) != 3 {
		t.Fatalf("drained %d events, want 3 (two output, one result)", len(events))
	}
	if events[0].Kind != KindToolOutput || events[0].Text != "one" {
		t.Errorf("event 0 = %+v, want a KindToolOutput carrying 'one'", events[0])
	}
	if events[1].Kind != KindToolOutput || events[1].Text != "two" {
		t.Errorf("event 1 = %+v, want a KindToolOutput carrying 'two'", events[1])
	}
	if events[2].Kind != KindToolResult || events[2].Tool.Result != "done" {
		t.Errorf("event 2 = %+v, want the final KindToolResult", events[2])
	}
	for _, ev := range events {
		if ev.Tool == nil || ev.Tool.ID != "c" || ev.Tool.Index != 2 {
			t.Errorf("event %+v lost the call's pairing (id/index)", ev)
		}
	}
}

// Sorting a batch is the only thing Drain does beyond emptying a slice, and
// deleting the sort outright left the suite green: nothing reported results
// out of order, so nothing noticed they came back that way.
func TestDrainReturnsABatchInTheOrderTheModelAskedFor(t *testing.T) {
	sink := newSink(Request{})
	for _, index := range []int{2, 0, 1} {
		sink.report(finished(index, "search"))
	}

	drained := sink.drain()
	if len(drained) != 3 {
		t.Fatalf("drained %d events, want 3", len(drained))
	}
	for position, event := range drained {
		if event.Tool.Index != position {
			t.Errorf("position %d holds index %d, want the model's order", position, event.Tool.Index)
		}
	}
}

// Two calls sharing an index is a backend that never filled it in, and the
// sort must leave them alone: completion order is the only order left, and the
// stable in SortStableFunc is the entire reason it survives. Twenty results
// across two indices is what it takes to see this — an unstable sort falls
// back to insertion sort below a dozen elements and looks stable by accident.
func TestDrainKeepsCompletionOrderForResultsSharingAnIndex(t *testing.T) {
	sink := newSink(Request{})
	for order := range 20 {
		sink.report(finished(order%2, fmt.Sprintf("tool%02d", order)))
	}

	latest := map[int]string{}
	for _, event := range sink.drain() {
		if before, seen := latest[event.Tool.Index]; seen && event.Tool.Name < before {
			t.Errorf("index %d returned %q after %q, want the order they finished in",
				event.Tool.Index, event.Tool.Name, before)
		}
		latest[event.Tool.Index] = event.Tool.Name
	}
}

// A provider's events carry a Tool pointer that can be nil. Sorting two of those dereferenced
// it and took the host down; one alone never did, because a single-element
// sort never calls the comparator, which made the crash intermittent.
func TestDrainSurvivesResultsCarryingNoTool(t *testing.T) {
	sink := newSink(Request{})
	sink.report(Event{Kind: KindToolResult})
	sink.report(Event{Kind: KindToolResult})

	if drained := sink.drain(); len(drained) != 2 {
		t.Errorf("drained %d events, want both kept rather than quietly dropped", len(drained))
	}
}

// Tool handlers run on their own goroutines while the event sequence is pulled
// from another, which is the entire reason this type holds a mutex. Nothing
// else in the package starts a second goroutine, so -race in the gate has been
// watching an empty road.
func TestReportAndDrainAreSafeFromSeveralGoroutinesAtOnce(t *testing.T) {
	const reported = 50

	sink := newSink(Request{})
	var runners sync.WaitGroup
	var counted sync.Mutex
	drained := 0

	for index := range reported {
		runners.Go(func() { sink.report(finished(index, "search")) })
	}
	for range 5 {
		runners.Go(func() {
			count := len(sink.drain())
			counted.Lock()
			defer counted.Unlock()
			drained += count
		})
	}

	runners.Wait()
	if drained += len(sink.drain()); drained != reported {
		t.Errorf("drained %d events, want every one of the %d reported", drained, reported)
	}
}
