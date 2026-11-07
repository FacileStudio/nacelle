package nacelle

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
)

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
