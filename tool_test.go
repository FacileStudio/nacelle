package nacelle_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/FacileStudio/nacelle"
)

type searchInput struct {
	Query string `json:"query" jsonschema:"required,description=What to look for"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=How many results"`
}

func TestToolSchemaComesFromTheStructTags(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	schema := tool.Schema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %#v", schema)
	}
	if _, found := properties["query"]; !found {
		t.Errorf("properties = %#v, want a query field", properties)
	}

	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("required = %#v, want query alone", required)
	}
	if _, leaked := schema["$schema"]; leaked {
		t.Error("the schema keyword survived; it is noise in a tool definition")
	}
}

// A model calls a tool by naming arguments, and a bare string has no names.
func TestToolInputMustBeAStruct(t *testing.T) {
	if _, err := nacelle.NewTool("bad", "takes a string", func(context.Context, string) (string, error) {
		return "", nil
	}); err == nil {
		t.Fatal("a non-struct input was accepted")
	}
}

func TestToolNeedsANameAndDescription(t *testing.T) {
	run := func(context.Context, searchInput) (string, error) { return "", nil }
	if _, err := nacelle.NewTool("", "described", run); err == nil {
		t.Error("a nameless tool was accepted")
	}
	if _, err := nacelle.NewTool("named", "", run); err == nil {
		t.Error("a tool with no description was accepted; the description is what the model chooses it by")
	}
}

func TestRunToolReportsThroughTheSink(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "found " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	result, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "call_1"}, json.RawMessage(`{"query":"x"}`), sink)
	if err != nil || result != "found x" {
		t.Fatalf("RunTool = %q, %v", result, err)
	}

	events := sink.Drain()
	if len(events) != 1 {
		t.Fatalf("drained %d events, want 1", len(events))
	}
	reported := events[0].Tool
	if reported.ID != "call_1" || reported.Name != "search" || reported.Result != "found x" {
		t.Errorf("reported = %+v, want the call identified and its result", reported)
	}
	if reported.Duration <= 0 {
		t.Error("no duration was recorded")
	}
	if len(sink.Drain()) != 0 {
		t.Error("a second drain returned events")
	}
}

// A tool failure is handed back to the model rather than ending the run.
func TestRunToolReportsAFailure(t *testing.T) {
	boom := errors.New("no such entity")
	tool, err := nacelle.NewTool("lookup", "Look things up", func(context.Context, searchInput) (string, error) {
		return "", boom
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	if _, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink); err == nil {
		t.Fatal("RunTool swallowed the error")
	}
	if events := sink.Drain(); len(events) != 1 || !errors.Is(events[0].Tool.Err, boom) {
		t.Errorf("the failure was not reported: %+v", events)
	}
}

// The model produced the arguments, so the model is who needs to hear they did
// not fit — it can usually fix them next turn.
func TestBadArgumentsAreReturnedNotPanicked(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, searchInput) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"query": 12}`)); err == nil {
		t.Fatal("arguments that do not match the schema were accepted")
	}
}

// Nil is the default, and the default has to mean what every tool already
// did before Approve existed: run unasked. A package that started refusing
// by default would break every consumer that never asked for a gate.
func TestANilApproveRunsEveryCallUnasked(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "found " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	sink := &nacelle.ToolSink{}
	result, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)
	if err != nil || result != "found x" {
		t.Fatalf("RunTool = %q, %v, want it to run without being asked", result, err)
	}
}

// A refusal is reported as a failed call, not skipped in silence — the
// pairing contract (a call started must be closed) still applies, and the
// model is better placed than this package to decide whether the task can
// still be finished without it.
func TestARefusedCallNeverRunsAndIsReportedAsRefused(t *testing.T) {
	ran := false
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, searchInput) (string, error) {
		ran = true
		return "should never happen", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	deny := func(context.Context, string, json.RawMessage) bool { return false }
	sink := &nacelle.ToolSink{Approve: deny}
	result, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink)

	if ran {
		t.Fatal("the tool ran despite being refused")
	}
	if err == nil || result != "" {
		t.Errorf("RunTool = %q, %v, want an error and no result", result, err)
	}
	events := sink.Drain()
	if len(events) != 1 || !events[0].Tool.Refused {
		t.Fatalf("events = %+v, want exactly one, marked Refused", events)
	}
	if events[0].Tool.Err == nil {
		t.Error("a refused call carried no error for the model to read")
	}
}

// approve is asked with the tool's name and its actual input, not a
// pre-decided answer — a gate that could not see what it was approving
// would not be one.
func TestApproveSeesTheToolNameAndInput(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "found " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	var gotName string
	var gotInput json.RawMessage
	approve := func(_ context.Context, name string, input json.RawMessage) bool {
		gotName, gotInput = name, input
		return true
	}

	sink := &nacelle.ToolSink{Approve: approve}
	if _, err := nacelle.RunTool(context.Background(), tool, nacelle.Invocation{ID: "c"}, json.RawMessage(`{"query":"x"}`), sink); err != nil {
		t.Fatalf("RunTool: %v", err)
	}
	if gotName != "search" {
		t.Errorf("name = %q, want search", gotName)
	}
	if string(gotInput) != `{"query":"x"}` {
		t.Errorf("input = %s, want the exact bytes the model sent", gotInput)
	}
}
