package client

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The shape a client normally sees, and the one that has to keep working.
// The server's own schema is hardened with additionalProperties: false, so
// the output carries that key even when the input did not.
func TestASchemaThatIsAlreadyAMapSurvivesTheConversion(t *testing.T) {
	raw := map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}}

	got, err := schemaOf(raw)
	if err != nil {
		t.Fatalf("schemaOf = %v, want it to succeed", err)
	}
	if got["type"] != "object" {
		t.Errorf("schemaOf has type = %q, want object", got["type"])
	}
	if got["additionalProperties"] != false {
		t.Errorf("schemaOf has additionalProperties = %v, want false", got["additionalProperties"])
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schemaOf.properties = %v, want a map", got["properties"])
	}
	query, ok := props["query"].(map[string]any)
	if !ok {
		t.Fatalf("schemaOf.properties.query = %v, want a map", props["query"])
	}
	if query["type"] != "string" {
		t.Errorf("schemaOf.properties.query.type = %q, want string", query["type"])
	}
}

// The reason this round-trips rather than type-asserting: a schema that
// arrives as raw JSON would otherwise become a nil map, and a model given no
// schema does not refuse to call the tool — it invents argument names.
func TestASchemaThatArrivesAsRawJSONIsDecodedRatherThanLost(t *testing.T) {
	got, err := schemaOf(json.RawMessage(`{"type":"object","required":["query"]}`))
	if err != nil {
		t.Fatalf("schemaOf = %v, want it to succeed", err)
	}
	if got["type"] != "object" {
		t.Errorf("schemaOf = %v, want a decoded object schema", got)
	}
}

// A tool that takes no arguments is a real thing, and both model APIs want an
// object either way.
func TestAMissingSchemaBecomesAnEmptyObject(t *testing.T) {
	got, err := schemaOf(nil)
	if err != nil {
		t.Fatalf("schemaOf = %v, want it to succeed", err)
	}
	if !reflect.DeepEqual(got, map[string]any{"type": "object"}) {
		t.Errorf("schemaOf = %v, want an empty object schema", got)
	}
}

// An array is not something a model can fill in, so it is refused at connect
// time rather than turning into a tool that fails the first time it is used.
func TestASchemaThatIsNotAnObjectIsRefused(t *testing.T) {
	if _, err := schemaOf([]string{"query"}); err == nil {
		t.Fatal("schemaOf accepted an array as an input schema")
	}
}
