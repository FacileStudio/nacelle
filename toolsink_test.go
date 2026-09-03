package nacelle_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// finished builds the event a backend parks in the sink for one tool that ran.
func finished(index int, name string) nacelle.Event {
	return nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{Index: index, Name: name}}
}

// Sorting a batch is the only thing Drain does beyond emptying a slice, and
// deleting the sort outright left the suite green: nothing reported results
// out of order, so nothing noticed they came back that way.
func TestDrainReturnsABatchInTheOrderTheModelAskedFor(t *testing.T) {
	sink := &nacelle.ToolSink{}
	for _, index := range []int{2, 0, 1} {
		sink.Report(finished(index, "search"))
	}

	drained := sink.Drain()
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
	sink := &nacelle.ToolSink{}
	for order := range 20 {
		sink.Report(finished(order%2, fmt.Sprintf("tool%02d", order)))
	}

	latest := map[int]string{}
	for _, event := range sink.Drain() {
		if before, seen := latest[event.Tool.Index]; seen && event.Tool.Name < before {
			t.Errorf("index %d returned %q after %q, want the order they finished in",
				event.Tool.Index, event.Tool.Name, before)
		}
		latest[event.Tool.Index] = event.Tool.Name
	}
}

// Report is exported for backend implementors, so the pointer it parks is
// written by a third party and can be nil. Sorting two of those dereferenced
// it and took the host down; one alone never did, because a single-element
// sort never calls the comparator, which made the crash intermittent.
func TestDrainSurvivesResultsCarryingNoTool(t *testing.T) {
	sink := &nacelle.ToolSink{}
	sink.Report(nacelle.Event{Kind: nacelle.KindToolResult})
	sink.Report(nacelle.Event{Kind: nacelle.KindToolResult})

	if drained := sink.Drain(); len(drained) != 2 {
		t.Errorf("drained %d events, want both kept rather than quietly dropped", len(drained))
	}
}

// Tool handlers run on their own goroutines while the event sequence is pulled
// from another, which is the entire reason this type holds a mutex. Nothing
// else in the package starts a second goroutine, so -race in the gate has been
// watching an empty road.
func TestReportAndDrainAreSafeFromSeveralGoroutinesAtOnce(t *testing.T) {
	const reported = 50

	sink := &nacelle.ToolSink{}
	var runners sync.WaitGroup
	var counted sync.Mutex
	drained := 0

	for index := range reported {
		runners.Add(1)
		go func() {
			defer runners.Done()
			sink.Report(finished(index, "search"))
		}()
	}
	for range 5 {
		runners.Add(1)
		go func() {
			defer runners.Done()
			count := len(sink.Drain())
			counted.Lock()
			defer counted.Unlock()
			drained += count
		}()
	}

	runners.Wait()
	if drained += len(sink.Drain()); drained != reported {
		t.Errorf("drained %d events, want every one of the %d reported", drained, reported)
	}
}
