package nacelle

import (
	"context"
	"encoding/json"
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

// Drain returns everything reported since the last call.
func (s *ToolSink) Drain() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	drained := s.pending
	s.pending = nil
	return drained
}

// RunTool executes a tool and reports the outcome to sink.
//
// Backends call this instead of calling Run directly, so that a tool result
// reaches the event stream the same way whichever backend executed it, and so
// that timing is measured in one place.
func RunTool(ctx context.Context, tool Tool, id string, input json.RawMessage, sink *ToolSink) (string, error) {
	started := time.Now()
	result, err := tool.Run(ctx, input)

	sink.Report(Event{
		Kind: KindToolResult,
		Tool: &ToolEvent{
			ID:       id,
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
