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
	Run(ctx context.Context, input json.RawMessage) (string, error)
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

	return &function[In]{name: name, description: description, schema: schema, run: run}, nil
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
}

func (f *function[In]) Name() string           { return f.name }
func (f *function[In]) Description() string    { return f.description }
func (f *function[In]) Schema() map[string]any { return f.schema }

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
