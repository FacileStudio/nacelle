package nacelle

import (
	"cmp"
	"slices"
	"sync"
)

// sink collects one run's tool events while the calls run.
//
// Tool handlers run on their own goroutines while the event sequence is
// pulled from a single one, so results are parked here and released between
// events rather than written from whichever goroutine produced them. wake is
// how the loop learns there is something to release without polling for it.
type sink struct {
	mu      sync.Mutex
	pending []Event
	wake    chan struct{}

	approve Approve
	hooks   map[HookPoint][]Hook

	// tools is the registry a call, and a Decision.Name redirect, is
	// resolved against. A name missing here refuses the call.
	tools map[string]Tool

	// yielded records that a tool returned EndTurn this run. Guarded by mu.
	yielded bool

	// denied remembers the tool names a hook has turned down this run, so
	// the next hook to see one of them can tell a first refusal from a
	// model retrying — the difference between enforcing a policy and
	// deny-looping it. Guarded by mu.
	denied map[string]bool
}

// newSink builds the sink for one run of request.
func newSink(request Request) *sink {
	return &sink{
		wake:    make(chan struct{}, 1),
		approve: request.Approve,
		hooks:   request.Hooks,
		tools:   ToolsByName(request.Tools),
	}
}

// report parks an event and wakes the loop, without ever blocking the tool
// goroutine that reported it.
func (s *sink) report(event Event) {
	s.mu.Lock()
	s.pending = append(s.pending, event)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// drain returns everything reported since the last call, in the order the
// model asked for it.
//
// Sorting is per batch, not across the whole run, and that is the honest
// limit: results are released as they land, so two tools that finish either
// side of a stream event arrive in separate batches and keep their completion
// order. ToolEvent.Index is the way out for anyone who needs the model's order
// regardless of when things finished.
func (s *sink) drain() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	drained := s.pending
	s.pending = nil
	slices.SortStableFunc(drained, func(a, b Event) int {
		return cmp.Compare(toolIndex(a), toolIndex(b))
	})
	return drained
}

// toolIndex is where an event sits in the turn that asked for it, and zero for
// an event carrying no tool at all.
func toolIndex(event Event) int {
	if event.Tool == nil {
		return 0
	}
	return event.Tool.Index
}

// markYielded records that a tool ended the turn.
func (s *sink) markYielded() {
	s.mu.Lock()
	s.yielded = true
	s.mu.Unlock()
}

// hasYielded reports whether a tool ended the turn this run.
func (s *sink) hasYielded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.yielded
}

// ToolsByName indexes tools by the name the model calls them by.
func ToolsByName(tools []Tool) map[string]Tool {
	index := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		index[tool.Name()] = tool
	}
	return index
}
