package nacelle

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/toolrunner"
)

// Tool is something the model can call.
//
// It is an alias for the SDK's interface rather than a type of our own: the
// SDK already defines exactly this shape, and wrapping it would buy a name and
// cost every consumer a conversion. Implement it directly for full control, or
// use NewTool, which is the ergonomic path.
type Tool = anthropic.BetaTool

// NewTool builds a tool from a Go function.
//
// The schema is generated from In's `json` and `jsonschema` struct tags, so a
// field is described where it is declared rather than in a JSON literal that
// drifts from it. The handler returns a string because that is what almost
// every tool has: text for the model to read.
//
// The description is prompt engineering, not documentation. Write it for a
// model that has never seen the codebase, and say what the tool is for rather
// than what it returns.
func NewTool[In any](name, description string, run func(ctx context.Context, in In) (string, error)) (Tool, error) {
	return toolrunner.NewBetaToolFromJSONSchema(
		name,
		description,
		func(ctx context.Context, in In) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			out, err := run(ctx, in)
			if err != nil {
				return anthropic.BetaToolResultBlockParamContentUnion{}, err
			}
			return anthropic.BetaToolResultBlockParamContentUnion{
				OfText: &anthropic.BetaTextBlockParam{Text: out},
			}, nil
		},
	)
}

// toolSink collects tool results as they happen.
//
// The runner executes tools itself, and it does so concurrently when the model
// asks for several at once, while the event stream is pulled from a single
// goroutine. So results are parked here and drained between stream events
// instead of being written to the stream from whichever goroutine produced
// them.
type toolSink struct {
	mu      sync.Mutex
	pending []Event
}

func (s *toolSink) push(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, event)
}

// drain returns everything collected since the last call.
func (s *toolSink) drain() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return nil
	}
	drained := s.pending
	s.pending = nil
	return drained
}

// observed wraps a tool so that running it also reports it.
//
// Wrapping is the only place the result of a tool call is visible: the runner
// owns execution and appends the result block itself, so a consumer watching
// the message stream sees the call and never the answer.
type observed struct {
	Tool
	sink *toolSink
}

func (o observed) Execute(ctx context.Context, input json.RawMessage) ([]anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	blocks, err := o.Tool.Execute(ctx, input)

	event := Event{
		Kind: KindToolResult,
		Tool: &ToolEvent{
			Name:     o.Tool.Name(),
			Input:    string(input),
			Err:      err,
			Duration: time.Since(started),
		},
	}
	for _, block := range blocks {
		if block.OfText != nil {
			event.Tool.Result += block.OfText.Text
		}
	}
	o.sink.push(event)

	return blocks, err
}

// observe wraps every tool so its results reach the stream.
func observe(tools []Tool, sink *toolSink) []Tool {
	if len(tools) == 0 {
		return nil
	}
	wrapped := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		wrapped = append(wrapped, observed{Tool: tool, sink: sink})
	}
	return wrapped
}
