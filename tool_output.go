package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
)

// outputFunction is a Tool that also implements OutputTool: it carries the
// plain function (Name, Description, Schema, Run) and an output-capable run
// that calls emit for each fragment of output as it is produced.
type outputFunction[In any] struct {
	*function[In]
	runOutput func(ctx context.Context, in In, emit func(string)) (string, error)
}

// RunOutput decodes the model's arguments and runs the output-capable function,
// forwarding each emitted fragment to the stream.
func (f *outputFunction[In]) RunOutput(ctx context.Context, input json.RawMessage, emit func(string)) (string, error) {
	var decoded In
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return "", fmt.Errorf("the arguments did not match the schema: %w", err)
		}
	}
	return f.runOutput(ctx, decoded, emit)
}

// NewOutputTool builds an output-streaming tool from a Go function that can
// emit its output as it is produced. The returned tool implements both Tool
// and OutputTool: plain backends treat it as a normal tool and see the single
// result; a consumer or backend that drains the sink while a tool runs sees
// each emitted fragment as a KindToolOutput event in the meantime.
func NewOutputTool[In any](name, description string, run func(ctx context.Context, in In, emit func(string)) (string, error)) (Tool, error) {
	if name == "" {
		return nil, fmt.Errorf("nacelle: a tool needs a name")
	}
	if description == "" {
		return nil, fmt.Errorf("nacelle: tool %q needs a description, which is what the model chooses it by", name)
	}

	schema, err := schemaOf[In]()
	if err != nil {
		return nil, fmt.Errorf("nacelle: tool %q: %w", name, err)
	}
	base := &function[In]{name: name, description: description, schema: schema, run: func(ctx context.Context, in In) (string, error) {
		return run(ctx, in, func(string) {})
	}}
	return &outputFunction[In]{function: base, runOutput: run}, nil
}
