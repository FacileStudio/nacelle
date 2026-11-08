package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
)

// Spec is everything the model and the loop know about a tool before it runs.
//
// Description is prompt engineering, not documentation: write it for a model
// that has never seen the codebase, and say what the tool is for rather than
// what it returns. Schema is the JSON Schema of the input, as a decoded
// object. ReadOnly declares that the tool never changes anything, which lets
// the loop run it beside other read-only calls. Hints carries the rest of what
// the tool's author claimed about it, and is nil when nobody claimed anything.
type Spec struct {
	Name        string
	Description string
	Schema      map[string]any
	ReadOnly    bool
	Hints       *Hints
}

// Hints are the behaviour claims an MCP server makes about a tool, its
// annotations, resolved against the protocol's defaults: a server that says
// nothing is destructive and open-world. They are claims, not guarantees, so a
// host trusts them only from a server it trusts.
//
// Destructive means a write may delete or overwrite. Idempotent means calling
// twice with the same arguments does nothing more than calling once.
// OpenWorld means the tool reaches beyond its own system, such as the web or
// a mailbox, so what it returns is untrusted input.
type Hints struct {
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
}

// Result is what a tool returned: the text the model reads and any pictures
// it reads beside it, such as a screenshot.
type Result struct {
	Text   string
	Images []Image
}

// RunFunc runs one call. emit streams a fragment of output while the call
// runs; each one reaches the stream as KindToolOutput and is never replayed
// into the conversation. emit is never nil when the loop calls it.
type RunFunc func(ctx context.Context, call Call, emit func(string)) (Result, error)

// Tool is something the model can call.
//
// The interface is this package's own rather than any SDK's, because a tool
// has to be callable by every backend. Every capability a tool has is in its
// Spec or its Result, so a decorator built with Wrap keeps all of them.
//
// Run reads call.Input, the raw JSON the model wrote. An error is not fatal:
// it is reported and handed back to the model, which is usually better placed
// to decide whether the task can still be finished. Run may be called from
// several goroutines at once: read-only calls in one round run together, and
// an Agent may be shared between requests.
type Tool interface {
	Spec() Spec
	Run(ctx context.Context, call Call, emit func(string)) (Result, error)
}

// NewTool builds a tool from a Go function.
//
// The schema is generated from In's `json` and `jsonschema` struct tags, so a
// field is described where it is declared rather than in a JSON literal that
// drifts from it:
//
//	type searchInput struct {
//	    Query string `json:"query" jsonschema:"required,description=What to look for"`
//	}
//
// In must be a struct. A model calls a tool by naming arguments, and a bare
// string or slice has no names to give.
func NewTool[In any](name, description string, run func(ctx context.Context, in In) (string, error)) (Tool, error) {
	return NewToolWithOptions(name, description, run, ToolOptions{})
}

// ToolOptions configures optional behavior for a tool created with NewToolWithOptions.
type ToolOptions struct {
	// ReadOnly declares that this tool never mutates state, so the loop may
	// run it beside other read-only calls.
	ReadOnly bool
}

// NewToolWithOptions builds a tool from a Go function with additional options.
func NewToolWithOptions[In any](name, description string, run func(ctx context.Context, in In) (string, error), opts ToolOptions) (Tool, error) {
	return newFunction(name, description, opts, func(ctx context.Context, in In, _ func(string)) (Result, error) {
		text, err := run(ctx, in)
		return Result{Text: text}, err
	})
}

// NewImageTool builds a tool whose answer includes pictures, such as a
// browser screenshot, with the schema generated from In as NewTool does.
func NewImageTool[In any](
	name, description string,
	run func(ctx context.Context, in In) (string, []Image, error),
	opts ToolOptions,
) (Tool, error) {
	return newFunction(name, description, opts, func(ctx context.Context, in In, _ func(string)) (Result, error) {
		text, images, err := run(ctx, in)
		return Result{Text: text, Images: images}, err
	})
}

// NewOutputTool builds a tool that streams its output as it is produced.
// Each fragment it emits reaches the stream as KindToolOutput; the complete
// return value still arrives whole on KindToolResult.
func NewOutputTool[In any](name, description string, run func(ctx context.Context, in In, emit func(string)) (string, error)) (Tool, error) {
	return newFunction(name, description, ToolOptions{}, func(ctx context.Context, in In, emit func(string)) (Result, error) {
		text, err := run(ctx, in, emit)
		return Result{Text: text}, err
	})
}

// newFunction validates a function tool's name and description and reflects
// its schema.
func newFunction[In any](name, description string, opts ToolOptions, run func(context.Context, In, func(string)) (Result, error)) (Tool, error) {
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
	spec := Spec{Name: name, Description: description, Schema: schema, ReadOnly: opts.ReadOnly}
	return &function[In]{spec: spec, run: run}, nil
}

// function is a Tool backed by a Go func.
type function[In any] struct {
	spec Spec
	run  func(ctx context.Context, in In, emit func(string)) (Result, error)
}

// Spec describes the function tool.
func (f *function[In]) Spec() Spec { return f.spec }

// Run decodes the model's arguments and calls the function.
//
// A decode failure is returned rather than panicking through the backend: the
// model produced the arguments, so the model is who needs to hear that they
// did not fit, and it can usually fix them on the next turn.
func (f *function[In]) Run(ctx context.Context, call Call, emit func(string)) (Result, error) {
	var decoded In
	if len(call.Input) > 0 {
		if err := json.Unmarshal(call.Input, &decoded); err != nil {
			return Result{}, fmt.Errorf("the arguments did not match the schema: %w", err)
		}
	}
	if emit == nil {
		emit = func(string) {}
	}
	return f.run(ctx, decoded, emit)
}
