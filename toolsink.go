package nacelle

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"
)

// ToolSink collects tool results for a backend to report.
//
// It is exported for backend implementors and is not otherwise interesting.
// Backends need it because tool handlers may run concurrently while an event
// sequence is pulled from a single goroutine, so results are parked here and
// released between events rather than written from whichever goroutine
// produced them.
type ToolSink struct {
	mu      sync.Mutex
	pending []Event
}

// Report records that a tool finished.
func (s *ToolSink) Report(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, event)
}

// Drain returns everything reported since the last call, in the order the
// model asked for it.
//
// Sorting is per batch, not across the whole run, and that is the honest
// limit: results are released as they land, so two tools that finish either
// side of a stream event arrive in separate batches and keep their completion
// order. Holding every result until the slowest tool in the turn returned
// would make the whole stream deterministic and would also stop the UI moving
// while the work happens, which is most of what a consumer wants the stream
// for. ToolEvent.Index is the way out for anyone who needs the model's order
// regardless of when things finished.
func (s *ToolSink) Drain() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	drained := s.pending
	s.pending = nil
	slices.SortStableFunc(drained, func(a, b Event) int {
		return a.Tool.Index - b.Tool.Index
	})
	return drained
}

// Invocation identifies one tool call within the turn that asked for it.
//
// It travels as a struct rather than as two more parameters because the two
// fields answer different questions and are wrong to mix up: ID is what pairs
// a result to its call across the stream, Index is where the model put it.
type Invocation struct {
	// ID is the provider's identifier for the call.
	ID string

	// Index is the call's position in the turn, from zero.
	Index int
}

// RunTool executes a tool and reports the outcome to sink.
//
// Backends call this instead of calling Run directly, so that a tool result
// reaches the event stream the same way whichever backend executed it, and so
// that timing is measured in one place.
func RunTool(ctx context.Context, tool Tool, call Invocation, input json.RawMessage, sink *ToolSink) (string, error) {
	started := time.Now()
	result, err := tool.Run(ctx, input)

	sink.Report(Event{
		Kind: KindToolResult,
		Tool: &ToolEvent{
			ID:       call.ID,
			Index:    call.Index,
			Name:     tool.Name(),
			Input:    string(input),
			Result:   result,
			Err:      err,
			Duration: time.Since(started),
		},
	})

	return result, err
}

// ToolsByName indexes tools for a backend dispatching a call by name.
func ToolsByName(tools []Tool) map[string]Tool {
	index := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		index[tool.Name()] = tool
	}
	return index
}
