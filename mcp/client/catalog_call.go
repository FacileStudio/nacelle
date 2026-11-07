package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FacileStudio/nacelle"
)

// catalogDetails is the catalog's get_tool_details.
type catalogDetails struct{ catalog *catalog }

// Spec describes get_tool_details.
func (catalogDetails) Spec() nacelle.Spec {
	return nacelle.Spec{
		Name: "get_tool_details",
		Description: "Return one tool's full description and input schema. Call search_tools first, then " +
			"this before the first call to any tool whose schema you have not read. Returns the schema " +
			"JSON, not an example call.",
		Schema: map[string]any{"type": "object", "required": []string{"name"}, "properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "The tool's exact name, as search_tools returned it."},
		}},
		ReadOnly: true,
	}
}

// Run returns the named tool's description and schema.
func (t catalogDetails) Run(_ context.Context, call nacelle.Call, _ func(string)) (nacelle.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(call.Input, &in); err != nil {
		return nacelle.Result{}, fmt.Errorf("reading name: %w", err)
	}
	tool, found := t.catalog.find(in.Name)
	if !found {
		return nacelle.Result{Text: fmt.Sprintf("No tool named %s. search_tools finds what exists.", in.Name)}, nil
	}
	spec := tool.Spec()
	schema, err := json.Marshal(spec.Schema)
	if err != nil {
		schema = []byte("{}")
	}
	return nacelle.Result{Text: fmt.Sprintf("%s\n\n%s\n\nInput schema:\n%s", spec.Name, spec.Description, schema)}, nil
}

// catalogCall is the catalog's call_tool, a Router to the tool it names.
type catalogCall struct{ catalog *catalog }

// Spec describes call_tool.
func (catalogCall) Spec() nacelle.Spec {
	return nacelle.Spec{
		Name: "call_tool",
		Description: "Run a tool by name with a JSON arguments object. The name must be one search_tools " +
			"surfaced; arguments must match the schema get_tool_details returned. Returns the tool's own " +
			"output, not a summary — pass it on, trimmed, when it answers the user.",
		Schema: map[string]any{"type": "object", "required": []string{"name"}, "properties": map[string]any{
			"name":      map[string]any{"type": "string", "description": "The tool's exact name, as search_tools returned it."},
			"arguments": map[string]any{"type": "object", "description": "The tool's input, matching the schema get_tool_details returned. Omitted when the schema has no properties."},
		}},
	}
}

// Route names the tool to run and the arguments it reads.
func (t catalogCall) Route(input json.RawMessage) (nacelle.Tool, json.RawMessage, error) {
	var in struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := decode(input, &in); err != nil {
		return nil, nil, fmt.Errorf("reading name and arguments: %w", err)
	}
	tool, found := t.catalog.find(in.Name)
	if !found {
		return nil, nil, fmt.Errorf("no tool named %s; run search_tools first", in.Name)
	}
	return tool, arguments(in.Arguments), nil
}

// Run routes the call and runs the named tool, for a caller outside the loop.
func (t catalogCall) Run(ctx context.Context, call nacelle.Call, emit func(string)) (nacelle.Result, error) {
	tool, input, err := t.Route(call.Input)
	if err != nil {
		return nacelle.Result{}, err
	}
	call.Name, call.Input = tool.Spec().Name, input
	return tool.Run(ctx, call, emit)
}

// arguments repairs the two shapes a model gets arguments wrong in: missing
// or null, which is an empty object, and an object written as a JSON string.
func arguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			return json.RawMessage(trimmed)
		}
	}
	return raw
}
