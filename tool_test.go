package nacelle

import (
	"context"
	"errors"
	"testing"
)

type echoInput struct {
	Say string `json:"say" jsonschema:"required,description=What to echo back"`
}

func TestNewToolGeneratesItsSchemaFromTheStruct(t *testing.T) {
	tool, err := NewTool("echo", "Echo a phrase back", func(_ context.Context, in echoInput) (string, error) {
		return in.Say, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	if tool.Name() != "echo" || tool.Description() != "Echo a phrase back" {
		t.Errorf("tool = %q/%q, want echo and its description", tool.Name(), tool.Description())
	}
	if _, ok := tool.InputSchema().Properties.(map[string]any)["say"]; !ok {
		t.Errorf("schema properties = %#v, want a say field from the struct tag", tool.InputSchema().Properties)
	}
}

// The runner owns execution and appends the result block itself, so wrapping
// is the only place a tool's answer is visible to a consumer.
func TestObservedToolReportsItsResult(t *testing.T) {
	tool, err := NewTool("echo", "Echo a phrase back", func(_ context.Context, in echoInput) (string, error) {
		return "you said " + in.Say, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	sink := &toolSink{}
	wrapped := observe([]Tool{tool}, sink)
	if _, err := wrapped[0].Execute(context.Background(), []byte(`{"say":"hi"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	events := sink.drain()
	if len(events) != 1 {
		t.Fatalf("running a tool reported %d events, want 1", len(events))
	}
	reported := events[0]
	if reported.Kind != KindToolResult {
		t.Errorf("kind = %q, want %q", reported.Kind, KindToolResult)
	}
	if reported.Tool.Name != "echo" || reported.Tool.Result != "you said hi" {
		t.Errorf("tool = %+v, want echo returning its answer", reported.Tool)
	}
	if reported.Tool.Input != `{"say":"hi"}` {
		t.Errorf("input = %q, want the raw arguments", reported.Tool.Input)
	}
	if reported.Tool.Duration <= 0 {
		t.Error("no duration was recorded")
	}
}

// A failing tool is reported and handed back to the model rather than ending
// the run: the model is usually better placed to decide whether the task can
// still be finished.
func TestObservedToolReportsAFailure(t *testing.T) {
	boom := errors.New("no such entity")
	tool, err := NewTool("lookup", "Look something up", func(_ context.Context, _ echoInput) (string, error) {
		return "", boom
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	sink := &toolSink{}
	wrapped := observe([]Tool{tool}, sink)
	if _, err := wrapped[0].Execute(context.Background(), []byte(`{"say":"x"}`)); err == nil {
		t.Fatal("Execute swallowed the tool's error")
	}

	events := sink.drain()
	if len(events) != 1 {
		t.Fatalf("a failing tool reported %d events, want 1", len(events))
	}
	if !errors.Is(events[0].Tool.Err, boom) {
		t.Errorf("reported error = %v, want %v", events[0].Tool.Err, boom)
	}
}

func TestObserveLeavesAnEmptyToolListAlone(t *testing.T) {
	if got := observe(nil, &toolSink{}); got != nil {
		t.Errorf("observe(nil) = %#v, want nil", got)
	}
}
