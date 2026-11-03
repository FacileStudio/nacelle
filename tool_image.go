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
	return &imageFunction[In]{function: base.(*function[In]), runImages: run}, nil
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
// that timing is measured in one place. It is also the one place that checks
// sink.Approve, so a refusal looks the same regardless of which backend asked.
// A refusal is reported as a failed call, not skipped in silence, and Refused
// tells a consumer it was a policy decision rather than the tool breaking.
func RunToolImages(ctx context.Context, tool Tool, call Invocation, input json.RawMessage, sink *ToolSink) (string, []Image, error) {
	if denied, reason := sink.runBeforeHooks(ctx, tool.Name(), input); denied {
		return "", nil, refuse(sink, tool, call, input, fmt.Errorf("nacelle: %q denied by hook: %s", tool.Name(), reason))
	}
	if sink.Approve != nil && !sink.Approve(ctx, tool.Name(), input) {
		return "", nil, refuse(sink, tool, call, input, fmt.Errorf("nacelle: %q was not approved to run", tool.Name()))
	}

	started := time.Now()
	result, images, err := invokeTool(ctx, tool, call, input, sink)
	result = sink.runAfterHooks(ctx, tool.Name(), input, result, err)
	event := toolResultEvent(tool, call, input, result, err)
	event.Tool.Duration = time.Since(started)
	sink.Report(event)
	return result, images, err
}

// invokeTool runs the tool through the richest interface it implements.
func invokeTool(ctx context.Context, tool Tool, call Invocation, input json.RawMessage, sink *ToolSink) (string, []Image, error) {
	switch tool := tool.(type) {
	case ImageTool:
		return tool.RunImages(ctx, input)
	case OutputTool:
		result, err := tool.RunOutput(ctx, input, func(fragment string) {
			if fragment == "" {
				return
			}
			sink.Report(Event{
				Kind: KindToolOutput,
				Tool: &ToolEvent{ID: call.ID, Index: call.Index, Name: tool.Name()},
				Text: fragment,
			})
		})
		return result, nil, err
	}
	result, err := tool.Run(ctx, input)
	return result, nil, err
}
