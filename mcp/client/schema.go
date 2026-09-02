package client

import (
	"encoding/json"
	"fmt"
)

// schemaOf turns a server's loosely typed input schema into the decoded
// object nacelle.Tool.Schema promises.
//
// The SDK types the field as any because a server may send anything that
// marshals to a schema, and it does not parse it on the way in. A client
// normally sees a map and the assertion would be the whole conversion — but a
// server that sent json.RawMessage, or an SDK that changes what it hands
// back, would then produce a tool whose schema arrives as nil. A model given
// no schema does not refuse to call the tool: it invents argument names. The
// round-trip costs one marshal per tool, once, at connect time, and is the
// price of never finding out which servers do that.
//
// A missing schema becomes an empty object rather than an error, because a
// tool that takes no arguments is a real thing and both model APIs require an
// object either way. Anything that is not an object is refused: an array or a
// string is not something a model can fill in.
func schemaOf(raw any) (map[string]any, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encoding the input schema: %w", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, fmt.Errorf("the input schema is not a JSON object: %w", err)
	}
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	hardenSchema(schema)
	return schema, nil
}

// hardenSchema sets additionalProperties: false on every object that declares
// properties without one, preventing the model from injecting fields the
// server did not define — a vector for prompt injection and confused-deputy
// attacks.
//
// It walks the full tree recursively: properties, items of arrays, and
// composition branches (via hardenComposition). A server that explicitly sets
// additionalProperties to true is left alone — the author chose that.
func hardenSchema(schema map[string]any) {
	properties, hasProps := schema["properties"]
	if !hasProps {
		return
	}
	props, ok := properties.(map[string]any)
	if !ok || len(props) == 0 {
		return
	}
	if _, set := schema["additionalProperties"]; !set {
		schema["additionalProperties"] = false
	}

	for _, prop := range props {
		if child, ok := prop.(map[string]any); ok {
			hardenSchema(child)
		}
	}

	if items, ok := schema["items"].(map[string]any); ok {
		hardenSchema(items)
	}
	hardenComposition(schema)
}

// hardenComposition recurses into anyOf/oneOf/allOf branches.
func hardenComposition(schema map[string]any) {
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		list, ok := schema[key].([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			if child, ok := item.(map[string]any); ok {
				hardenSchema(child)
			}
		}
	}
}
