package nacelle

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
)

// Tool is something the model can call.
//
// The interface is this package's own rather than any SDK's, because a tool
// has to be callable by every backend. An Anthropic-shaped tool type in the
// core would mean the OpenRouter backend converting from a vocabulary that has
// nothing to do with it.
type Tool interface {
	// Name is what the model calls. It must be unique within one agent.
	Name() string

	// Description is prompt engineering, not documentation. Write it for a
	// model that has never seen the codebase, and say what the tool is for
	// rather than what it returns.
	Description() string

	// Schema is the JSON Schema of the tool's input, as a decoded object.
	Schema() map[string]any

	// Run executes the tool. The string it returns is what the model reads,
	// so it should be text a reader could follow, not a debug dump.
	//
	// An error is not fatal: it is reported to the caller and handed back to
	// the model, which is usually better placed to decide whether the task
	// can still be finished.
	//
	// Run may be called from several goroutines at once, and an
	// implementation has to be ready for it. A model can ask for two tools
	// in one turn and a backend runs those together, so this happens on a
	// single conversation before anything shares an Agent between
	// requests. A tool that keeps a field between calls needs its own
	// lock; a tool that only reads what it was built with needs nothing,
	// which is why every tool in tools/ and mcp/client is the second kind.
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

// ReadOnlyTool is an optional interface a Tool may implement to declare
// that it never mutates state. Backends that support the ToolCallPlanner
// capability use this to sequence read-only calls before write calls,
// reducing context bloat by batching independent tools into fewer turns.
//
// If a tool does not implement this interface, it is treated as potentially
// mutating and will be run after all read-only tools in the batch.
type ReadOnlyTool interface {
	// IsReadOnly reports whether this tool only reads and never writes.
	IsReadOnly() bool
}

// Approve decides whether a tool call may run, asked once per call before
// RunTool ever calls Run.
//
// Nil is the default and means every call runs unasked — the same behaviour
// this package has always had. Most consumers (a server, a CI job, an
// unattended run) have nobody to ask, and a package that refused by default
// would make every one of them write a rubber-stamp callback just to get
// back to how every tool already worked. A consumer that wants a human in
// the loop sets this; nothing else about Tool or RunTool changes for one
// that does not.
//
// It is asked with the same context RunTool receives, so cancelling a run
// (a caller abandoning the stream) unblocks anyone waiting on an answer that
// is never coming, the same way it already unblocks a tool mid-Run.
//
// It may be asked from several goroutines at once, for the reason Tool.Run
// documents, and a callback that puts a question to a person has to do
// something about that rather than assume it. tui/ answers it by serialising
// the prompts: two questions racing for one terminal is one question nobody
// can read, and neither answer belongs to the call it lands on.
type Approve func(ctx context.Context, name string, input json.RawMessage) bool

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
	return NewToolWithOptions[In](name, description, run, ToolOptions{})
}

// ToolOptions configures optional behavior for a tool created with NewToolWithOptions.
type ToolOptions struct {
	// ReadOnly declares that this tool never mutates state. Backends with
	// the ToolCallPlanner capability use this to batch and sequence calls.
	ReadOnly bool
}

// NewToolWithOptions builds a tool from a Go function with additional options.
func NewToolWithOptions[In any](name, description string, run func(ctx context.Context, in In) (string, error), opts ToolOptions) (Tool, error) {
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

	return &function[In]{name: name, description: description, schema: schema, run: run, readOnly: opts.ReadOnly}, nil
}

// schemaOf reflects In into a JSON Schema object.
//
// The reflector is told not to hoist definitions into $defs and not to add a
// $ref indirection: a tool schema is sent inline in a request, and a document
// whose real content sits behind a reference to a sibling definition is one
// more thing between the model and the argument names.
func schemaOf[In any]() (map[string]any, error) {
	var zero In
	if kind := reflect.TypeOf(&zero).Elem().Kind(); kind != reflect.Struct {
		return nil, fmt.Errorf("input must be a struct, not %s: a model names its arguments", kind)
	}

	reflector := jsonschema.Reflector{
		DoNotReference:             true,
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
	}
	encoded, err := json.Marshal(reflector.Reflect(&zero))
	if err != nil {
		return nil, fmt.Errorf("encoding the input schema: %w", err)
	}

	schema := map[string]any{}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, fmt.Errorf("decoding the input schema: %w", err)
	}
	delete(schema, "$schema")
	delete(schema, "$id")
	return schema, nil
}

// function is a Tool backed by a Go func.
type function[In any] struct {
	name        string
	description string
	schema      map[string]any
	run         func(ctx context.Context, in In) (string, error)
	readOnly    bool
}

func (f *function[In]) Name() string           { return f.name }
func (f *function[In]) Description() string    { return f.description }
func (f *function[In]) Schema() map[string]any { return f.schema }
func (f *function[In]) IsReadOnly() bool       { return f.readOnly }

// Run decodes the model's arguments and calls the function.
//
// A decode failure is returned rather than panicking through the backend: the
// model produced the arguments, so the model is who needs to hear that they
// did not fit, and it can usually fix them on the next turn.
func (f *function[In]) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var decoded In
	if len(input) > 0 {
		if err := json.Unmarshal(input, &decoded); err != nil {
			return "", fmt.Errorf("the arguments did not match the schema: %w", err)
		}
	}
	return f.run(ctx, decoded)
}
