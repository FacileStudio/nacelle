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

	schema := tool.Spec().Schema
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

func TestACallIsReportedOnTheStream(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(_ context.Context, in searchInput) (string, error) {
		return "found " + in.Query, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	got := runTool(t, context.Background(), nacelle.Request{}, tool, `{"query":"x"}`)
	if got.err != nil || got.result != "found x" {
		t.Fatalf("ran = %q, %v", got.result, got.err)
	}

	events := got.toolEvents()
	if len(events) != 1 {
		t.Fatalf("saw %d tool events, want 1", len(events))
	}
	reported := events[0].Tool
	if reported.ID == "" || reported.Name != "search" || reported.Result != "found x" {
		t.Errorf("reported = %+v, want the call identified and its result", reported)
	}
	if reported.Duration <= 0 {
		t.Error("no duration was recorded")
	}
}

// A tool failure is handed back to the model rather than ending the run.
func TestAFailedCallIsReported(t *testing.T) {
	boom := errors.New("no such entity")
	tool, err := nacelle.NewTool("lookup", "Look things up", func(context.Context, searchInput) (string, error) {
		return "", boom
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	got := runTool(t, context.Background(), nacelle.Request{}, tool, `{"query":"x"}`)
	if !errors.Is(got.err, boom) || got.result != boom.Error() {
		t.Errorf("the failure was not reported: %+v", got.tool)
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
	if _, err := callTool(context.Background(), tool, json.RawMessage(`{"query": 12}`)); err == nil {
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

	got := runTool(t, context.Background(), nacelle.Request{}, tool, `{"query":"x"}`)
	if got.err != nil || got.result != "found x" {
		t.Fatalf("ran = %q, %v, want it to run without being asked", got.result, got.err)
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

	deny := nacelle.ApproveBool(func(context.Context, string, json.RawMessage) bool { return false })
	got := runTool(t, context.Background(), nacelle.Request{Approve: deny}, tool, `{"query":"x"}`)

	if ran {
		t.Fatal("the tool ran despite being refused")
	}
	if got.err == nil || got.result != got.err.Error() {
		t.Errorf("ran = %q, %v, want the model to read the refusal", got.result, got.err)
	}
	events := got.toolEvents()
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
	approve := func(_ context.Context, call nacelle.Call) nacelle.Decision {
		gotName, gotInput = call.Name, call.Input
		return nacelle.Decision{}
	}

	if got := runTool(t, context.Background(), nacelle.Request{Approve: approve}, tool, `{"query":"x"}`); got.err != nil {
		t.Fatalf("ran: %v", got.err)
	}
	if gotName != "search" {
		t.Errorf("name = %q, want search", gotName)
	}
	if string(gotInput) != `{"query":"x"}` {
		t.Errorf("input = %s, want the exact bytes the model sent", gotInput)
	}
}
