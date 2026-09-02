package nacelle_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"

	"github.com/FacileStudio/nacelle"
)

// loop is a backend that plays the agent loop itself: each Stream call is
// one agent run, and it walks its scripted steps in order, running tools
// through RunTool exactly as a real backend would until a step answers. It
// is how delegation is tested end to end without a network — the first run
// belongs to the parent and asks for the subagent tool; the second is the
// nested agent on this same backend, answering its own script.
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

// step is one model turn: either asking for a tool — answer carries the raw
// input the tool gets — or giving one as text, with the stop reason a real
// response would have ended on.
type step struct {
	tool   string
	input  string
	answer string
	stop   nacelle.Stop
}

func toolStep(tool, input string) step { return step{tool: tool, input: input} }

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
	steps := []step{}
	if index < len(l.runs) {
		steps = l.runs[index]
	}
	sink := &nacelle.ToolSink{Approve: request.Approve}
	byName := nacelle.ToolsByName(request.Tools)
	l.mu.Unlock()

	return func(yield func(nacelle.Event, error) bool) {
		for _, s := range steps {
			if !l.playStep(s, sink, byName, index, yield) {
				return
			}
		}
	}
}

// playStep emits one step: a tool call runs the tool through the sink and
// drains the results; a text step answers and ends the stream.
func (l *loop) playStep(s step, sink *nacelle.ToolSink, byName map[string]nacelle.Tool, index int, yield func(nacelle.Event, error) bool) bool {
	if s.tool == "" {
		stop := s.stop
		if stop == "" {
			stop = nacelle.StopEnd
		}
		if !yield(nacelle.Event{Kind: nacelle.KindText, Text: s.answer}, nil) {
			return false
		}
		yield(nacelle.Event{Kind: nacelle.KindDone, Stop: stop}, nil)
		return false
	}
	id := fmt.Sprintf("call_%d_%s", index, s.tool)
	if !yield(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{ID: id, Name: s.tool, Input: s.input}}, nil) {
		return false
	}
	if tool, ok := byName[s.tool]; ok {
		nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: id}, json.RawMessage(s.input), sink)
	} else {
		sink.Report(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{ID: id, Name: s.tool, Input: s.input, Err: fmt.Errorf("no such tool %q", s.tool)}})
	}
	for _, drained := range sink.Drain() {
		if !yield(drained, nil) {
			return false
		}
	}
	return true
}