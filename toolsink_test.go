package nacelle_test

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// TestPlanCallsSequencesReadOnlyBeforeWrite verifies that read-only tools
// are placed before mutating ones in the planned execution order.
func TestPlanCallsSequencesReadOnlyBeforeWrite(t *testing.T) {
	readTool, err := nacelle.NewToolWithOptions("search", "Find things",
		func(context.Context, struct{}) (string, error) { return "", nil },
		nacelle.ToolOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("NewToolWithOptions: %v", err)
	}
	writeTool, err := nacelle.NewTool("run", "Do something",
		func(context.Context, struct{}) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	byName := nacelle.ToolsByName([]nacelle.Tool{readTool, writeTool})
	calls := []nacelle.Invocation{
		{ID: "run"},
		{ID: "search"},
		{ID: "run"},
	}

	planned := nacelle.PlanCalls(calls, byName)

	if len(planned) != 3 {
		t.Fatalf("len(planned) = %d, want 3", len(planned))
	}
	// First call should be read-only
	if planned[0].ID != "search" {
		t.Errorf("planned[0].ID = %q, want %q", planned[0].ID, "search")
	}
	// Remaining should be writes
	for i, call := range planned[1:] {
		if call.ID == "search" {
			t.Errorf("planned[%d] = %q, expected write tool", i+1, call.ID)
		}
	}
}

// TestPlanCallsUnknownToolsGoToWrite ensures unknown tools are treated as
// potentially mutating and placed after read-only ones.
func TestPlanCallsUnknownToolsGoToWrite(t *testing.T) {
	readTool, err := nacelle.NewToolWithOptions("search", "Find things",
		func(context.Context, struct{}) (string, error) { return "", nil },
		nacelle.ToolOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("NewToolWithOptions: %v", err)
	}

	byName := nacelle.ToolsByName([]nacelle.Tool{readTool})
	calls := []nacelle.Invocation{
		{ID: "unknown"},
		{ID: "search"},
		{ID: "unknown"},
	}

	planned := nacelle.PlanCalls(calls, byName)

	if len(planned) != 3 {
		t.Fatalf("len(planned) = %d, want 3", len(planned))
	}
	if planned[0].ID != "search" {
		t.Errorf("planned[0].ID = %q, want %q", planned[0].ID, "search")
	}
}
