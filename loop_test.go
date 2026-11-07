package nacelle_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// loop is a backend whose every Stream call is one agent run on its own
// script, driven through nacelle.NewBackend so tools run on the real loop. It
// is how delegation is tested end to end without a network — the first run
// belongs to the parent and asks for the parallel_agents tool; the later
// runs are the nested agents on this same backend, answering their own
// scripts.
type loop struct {
	mu       sync.Mutex
	runs     [][]step
	requests []nacelle.Request
	next     int
}

func (l *loop) Name() string                       { return "loop" }
func (l *loop) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (l *loop) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}

// step is one model turn: either asking for a tool — input is the raw input
// the tool gets — or answering as text, with the stop reason a real response
// would have ended on.
type step struct {
	tool   string
	inputs []string
	round  []nacelle.ToolCall
	answer string
	stop   nacelle.Stop
}

// roundStep asks for every call in one turn, each to its own tool.
func roundStep(calls ...nacelle.ToolCall) step { return step{round: calls} }

// toolStep asks for tool once per input, all in the one turn.
func toolStep(tool string, inputs ...string) step { return step{tool: tool, inputs: inputs} }

func textStep(answer string, stop ...nacelle.Stop) step {
	s := step{answer: answer}
	if len(stop) > 0 {
		s.stop = stop[0]
	}
	return s
}

func newLoop(runs ...[]step) *loop { return &loop{runs: runs} }

func (l *loop) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	l.mu.Lock()
	index := l.next
	l.next++
	l.requests = append(l.requests, request)
	var steps []step
	if index < len(l.runs) {
		steps = l.runs[index]
	}
	l.mu.Unlock()
	return nacelle.NewBackend(&script{run: index, steps: steps}).Stream(ctx, request)
}

// script is a Provider that plays one step per turn, and answers with an
// empty finished turn once it runs out.
type script struct {
	mu    sync.Mutex
	run   int
	steps []step
	turn  int
}

func (s *script) Name() string                       { return "script" }
func (s *script) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }
func (s *script) CountTokens(context.Context, nacelle.Request) (int64, error) {
	return 0, nil
}

func (s *script) Turn(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	s.mu.Lock()
	played := textStep("")
	if s.turn < len(s.steps) {
		played = s.steps[s.turn]
	}
	id := fmt.Sprintf("call_%d_%d_%s", s.run, s.turn, played.tool)
	s.turn++
	s.mu.Unlock()
	return func(yield func(nacelle.Event, error) bool) {
		for _, event := range played.events(id) {
			if !yield(event, nil) {
				return
			}
		}
	}
}

// events is the turn a provider streams for this step.
func (s step) events(id string) []nacelle.Event {
	if s.tool == "" && len(s.round) == 0 {
		stop := s.stop
		if stop == "" {
			stop = nacelle.StopEnd
		}
		answer := nacelle.AssistantText(s.answer)
		return []nacelle.Event{
			{Kind: nacelle.KindText, Text: s.answer},
			{Kind: nacelle.KindTurn, Stop: stop},
			{Kind: nacelle.KindMessage, Message: &answer},
		}
	}
	asked := nacelle.Message{Role: nacelle.RoleAssistant}
	var events []nacelle.Event
	for index, call := range s.calls(id) {
		asked.Parts = append(asked.Parts, call)
		events = append(events, nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{
			ID: call.ID, Index: index, Name: call.Name, Input: string(call.Input),
		}})
	}
	return append(events,
		nacelle.Event{Kind: nacelle.KindTurn, Stop: nacelle.StopTools},
		nacelle.Event{Kind: nacelle.KindMessage, Message: &asked},
	)
}

// calls are the tool calls this step asks for, identified from id.
func (s step) calls(id string) []nacelle.ToolCall {
	if len(s.round) > 0 {
		return s.round
	}
	calls := make([]nacelle.ToolCall, 0, len(s.inputs))
	for index, input := range s.inputs {
		callID := id
		if index > 0 {
			callID = fmt.Sprintf("%s_%d", id, index)
		}
		calls = append(calls, nacelle.ToolCall{ID: callID, Name: s.tool, Input: json.RawMessage(input), Finished: true})
	}
	return calls
}

// ran is one tool call played through the agent loop: what the model read,
// the result event the consumer saw, and every event of the run.
type ran struct {
	result string
	err    error
	tool   nacelle.ToolEvent
	events []nacelle.Event
	stop   nacelle.Stop
}

// runTool plays one call to tool with input through the loop, under the
// policy request carries. request.Tools defaults to tool alone.
func runTool(t *testing.T, ctx context.Context, request nacelle.Request, tool nacelle.Tool, input string) ran {
	t.Helper()
	return runSteps(t, ctx, request, tool, toolStep(tool.Spec().Name, input), textStep("done"))
}

// runSteps plays steps through the loop with tool available, and records the
// last call's outcome.
func runSteps(t *testing.T, ctx context.Context, request nacelle.Request, tool nacelle.Tool, steps ...step) ran {
	t.Helper()
	if request.Tools == nil {
		request.Tools = []nacelle.Tool{tool}
	}
	backend := nacelle.NewBackend(&script{steps: steps})
	var out ran
	for event, err := range backend.Stream(ctx, request) {
		if err != nil {
			t.Fatalf("the run failed: %v", err)
		}
		out.events = append(out.events, event)
		out.observe(event)
	}
	return out
}

// observe records what a test of one call wants out of an event.
func (r *ran) observe(event nacelle.Event) {
	switch event.Kind {
	case nacelle.KindToolResult:
		r.tool, r.err = *event.Tool, event.Tool.Err
	case nacelle.KindMessage:
		for _, part := range event.Message.Parts {
			if result, ok := part.(nacelle.ToolResult); ok {
				r.result = result.Result
			}
		}
	case nacelle.KindDone:
		r.stop = event.Stop
	}
}

// toolEvents are the run's tool results and output, in arrival order.
func (r ran) toolEvents() []nacelle.Event {
	var events []nacelle.Event
	for _, event := range r.events {
		if event.Kind == nacelle.KindToolResult || event.Kind == nacelle.KindToolOutput {
			events = append(events, event)
		}
	}
	return events
}

// callTool runs tool once outside the loop and returns the text it answered.
func callTool(ctx context.Context, tool nacelle.Tool, input json.RawMessage) (string, error) {
	result, err := tool.Run(ctx, nacelle.Call{Name: tool.Spec().Name, Input: input}, nil)
	return result.Text, err
}
