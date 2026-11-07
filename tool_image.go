package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
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
