package nacelle

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"sync"
)

// ToolSink collects tool results for a backend to report.
//
// It is exported for backend implementors and is not otherwise interesting.
// Backends need it because tool handlers may run concurrently while an event
// sequence is pulled from a single goroutine, so results are parked here and
// released between events rather than written from whichever goroutine
// produced them.
//
// Approve lives here rather than as its own parameter on RunTool: every
// caller already constructs and threads a ToolSink through to the same
// place, so a second piece of per-run policy travelling the same path is
// one field, not a second argument every call site has to carry.
type ToolSink struct {
	mu      sync.Mutex
	pending []Event

	Approve Approve
	Hooks   map[HookPoint][]Hook

	// Tools is the registry a Decision.Name redirect is resolved against.
	// A redirect to a name missing here refuses the call.
	Tools map[string]Tool

	// yielded records that a tool returned EndTurn this run. Guarded by mu.
	yielded bool

	// denied remembers the tool names a hook has turned down this run, so
	// the next hook to see one of them can tell a first refusal from a
	// model retrying — the difference between enforcing a policy and
	// deny-looping it. Guarded by mu.
	denied map[string]bool
}

// Report records that a tool finished.
//
// It takes whatever a backend hands it, an event carrying no ToolEvent
// included. Refusing one here would be the tidier trust boundary, but there is
// nowhere to put the refusal: the signature returns nothing, the caller is
// usually a tool goroutine with no consumer to hand an error to, and dropping
// the event silently loses a result the model is still waiting on. Drain is
// written to cope with it instead, which keeps the bad event visible.
func (s *ToolSink) Report(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, event)
}

// Yielded reports whether a tool returned EndTurn during this run. A backend
// checks it after a round of calls has finished and, when true, ends the run
// with StopYield instead of asking the model for another turn.
func (s *ToolSink) Yielded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.yielded
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
		return cmp.Compare(toolIndex(a), toolIndex(b))
	})
	return drained
}

// toolIndex is where an event sits in the turn that asked for it, and zero for
// an event carrying no tool at all.
//
// Report is exported, so the pointer it parks here belongs to whoever wrote
// the backend and can be nil. Reading Index straight off it crashed the host
// from inside a sort comparator, which is both the least debuggable place for
// a nil dereference and an intermittent one: a single pending event never
// calls the comparator, so the crash needed two tools to finish between
// events. Comparing rather than subtracting is the same fix twice over —
// subtracting two indices overflows for values a provider could in principle
// send, and there is no reason to hand-roll what cmp does.
func toolIndex(event Event) int {
	if event.Tool == nil {
		return 0
	}
	return event.Tool.Index
}

// Invocation identifies one tool call within the turn that asked for it.
//
// It travels as a struct rather than as two more parameters because the two
// fields answer different questions and are wrong to mix up: ID is what pairs
// a result to its call across the stream, Index is where the model put it.
type Invocation struct {
	// ID is the provider's identifier for the call.
	ID string

	// Name is the tool name being invoked.
	Name string

	// Index is the call's position in the turn, from zero.
	Index int
}

// RunTool executes a tool and reports the outcome to sink. It is
// RunToolImages with any pictures dropped, for a backend that has no slot
// for them.
//
// Backends call this instead of calling Run directly, so that a tool result
// reaches the event stream the same way whichever backend executed it, and so
// that timing is measured in one place. It is also the one place that checks
// sink.Approve, so a refusal looks the same — same event shape, same error
// returned to the caller — regardless of which backend asked.
//
// A refusal is reported as a failed call, not skipped in silence: the
// pairing contract (a call started must be closed) is the same one Discarded
// exists for, and the model is better placed than this package to decide
// whether the task can still be finished without it. Refused is what tells a
// consumer this was a policy decision, not the tool breaking.
func RunTool(ctx context.Context, tool Tool, call Invocation, input json.RawMessage, sink *ToolSink) (string, error) {
	result, _, err := RunToolImages(ctx, tool, call, input, sink)
	return result, err
}

// refuse reports a call that policy stopped before the tool ran, and hands
// the caller the same error the model will read.
//
// A refusal is reported as a failed call, not skipped in silence: the pairing
// contract (a call started must be closed) is the same one Discarded exists
// for. Refused is what tells a consumer this was a policy decision, not the
// tool breaking.
func refuse(sink *ToolSink, call Call, err error) error {
	event := toolResultEvent(call, err.Error(), err)
	event.Tool.Refused = true
	sink.Report(event)
	return err
}

// toolResultEvent builds the one event shape every outcome of a tool call
// arrives as, naming the tool and input that actually ran. The refusal path
// mutates Refused on its copy.
func toolResultEvent(call Call, result string, err error) Event {
	return Event{
		Kind: KindToolResult,
		Tool: &ToolEvent{
			ID: call.ID, Index: call.Index, Name: call.Name,
			Input: string(call.Input), Result: result, Err: err,
		},
	}
}

// ToolsByName indexes tools for a backend dispatching a call by name.
func ToolsByName(tools []Tool) map[string]Tool {
	index := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		index[tool.Name()] = tool
	}
	return index
}

// PlanCalls orders a batch of tool calls for execution, grouping read-only
// calls separately from mutating ones. Backends with the ToolCallPlanner
// capability can use this to batch independent calls into a single turn
// while ensuring write calls run after all read-only calls complete.
//
// The returned slices contain the original Invocations in execution order:
// read-only calls first, then mutating calls.
func PlanCalls(calls []Invocation, byName map[string]Tool) []Invocation {
	readOnly := make([]Invocation, 0, len(calls))
	write := make([]Invocation, 0, len(calls))

	for _, call := range calls {
		var tool Tool
		if call.Name != "" {
			tool = byName[call.Name]
		}
		if tool == nil {
			tool = byName[call.ID]
		}
		if ro, ok := tool.(ReadOnlyTool); ok && ro.IsReadOnly() {
			readOnly = append(readOnly, call)
			continue
		}
		write = append(write, call)
	}

	return append(readOnly, write...)
}
