package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ImageTool is an optional interface a Tool may implement when its answer
// includes pictures, such as a browser screenshot.
//
// The text is what the model reads, exactly as for Run; the images travel
// beside it on the ToolResult. Run stays the fallback a consumer that calls
// a tool directly gets, and it returns the text alone.
type ImageTool interface {
	Tool
	RunImages(ctx context.Context, input json.RawMessage) (string, []Image, error)
}

// NewImageTool builds an ImageTool from a Go function, with the schema
// generated from In exactly as NewTool does.
func NewImageTool[In any](
	name, description string,
	run func(ctx context.Context, in In) (string, []Image, error),
	opts ToolOptions,
) (Tool, error) {
	text := func(ctx context.Context, in In) (string, error) {
		result, _, err := run(ctx, in)
		return result, err
	}
	base, err := NewToolWithOptions(name, description, text, opts)
	if err != nil {
		return nil, err
	}
	fn, ok := base.(*function[In])
	if !ok {
		return nil, fmt.Errorf("nacelle: tool %q is not a function tool", name)
	}
	return &imageFunction[In]{function: fn, runImages: run}, nil
}

// imageFunction is a function tool that also returns pictures.
type imageFunction[In any] struct {
	*function[In]
	runImages func(ctx context.Context, in In) (string, []Image, error)
}

// RunImages decodes the model's arguments and calls the function.
func (f *imageFunction[In]) RunImages(ctx context.Context, input json.RawMessage) (string, []Image, error) {
	var decoded In
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return "", nil, fmt.Errorf("the arguments did not match the schema: %w", err)
		}
	}
	return f.runImages(ctx, decoded)
}

// RunToolImages executes a tool and reports the outcome to sink, returning
// the pictures an ImageTool produced beside its text.
//
// Backends call this instead of calling Run directly, so that a tool result
// reaches the event stream the same way whichever backend executed it, and so
// that timing is measured in one place. It is also the one place that runs
// the BeforeToolCall hooks and sink.Approve, so a refusal, an edited input or
// a redirect looks the same regardless of which backend asked. A refusal is
// reported as a failed call, not skipped in silence, and Refused tells a
// consumer it was a policy decision rather than the tool breaking.
//
// A tool that returns EndTurn is recorded as a success with the result it
// carried, and sink.Yielded reports true from then on.
func RunToolImages(ctx context.Context, tool Tool, call Invocation, input json.RawMessage, sink *ToolSink) (string, []Image, error) {
	ruled, err := sink.rule(ctx, tool, Call{ID: call.ID, Name: tool.Name(), Index: call.Index, Input: input})
	if err != nil {
		return "", nil, refuse(sink, ruled.call, err)
	}

	started := time.Now()
	result, images, err := invokeTool(ctx, ruled.tool, ruled.call, sink)
	result, err = sink.settle(result, err, ruled.note)
	result = sink.runAfterHooks(ctx, ruled.call, result, err)
	event := toolResultEvent(ruled.call, result, err)
	event.Tool.Duration = time.Since(started)
	sink.Report(event)
	return result, images, err
}

// settle turns an EndTurn into the successful result it carries, marking the
// run as yielded, and prepends the reviewer's note to what the model reads.
func (s *ToolSink) settle(result string, err error, note string) (string, error) {
	if ended, ok := endedTurn(err); ok {
		result, err = ended, nil
		s.mu.Lock()
		s.yielded = true
		s.mu.Unlock()
	}
	switch {
	case note == "":
		return result, err
	case err != nil:
		return result, fmt.Errorf("%s\n\n%w", note, err)
	}
	return note + "\n\n" + result, nil
}

// invokeTool runs the tool through the richest interface it implements.
func invokeTool(ctx context.Context, tool Tool, call Call, sink *ToolSink) (string, []Image, error) {
	switch tool := tool.(type) {
	case ImageTool:
		return tool.RunImages(ctx, call.Input)
	case OutputTool:
		result, err := tool.RunOutput(ctx, call.Input, func(fragment string) {
			if fragment == "" {
				return
			}
			sink.Report(Event{
				Kind: KindToolOutput,
				Tool: &ToolEvent{ID: call.ID, Index: call.Index, Name: call.Name},
				Text: fragment,
			})
		})
		return result, nil, err
	}
	result, err := tool.Run(ctx, call.Input)
	return result, nil, err
}
