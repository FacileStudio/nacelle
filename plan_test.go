package nacelle_test

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/tools"
)

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
	if planned[0].ID != "search" {
		t.Errorf("planned[0].ID = %q, want %q", planned[0].ID, "search")
	}
	for i, call := range planned[1:] {
		if call.ID == "search" {
			t.Errorf("planned[%d] = %q, expected write tool", i+1, call.ID)
		}
	}
}

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

func TestPlanCallsUsesNameWithFallbackToID(t *testing.T) {
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
		{ID: "call_1", Name: "run"},
		{ID: "call_2", Name: "search"},
		{ID: "run"},
	}

	planned := nacelle.PlanCalls(calls, byName)
	if len(planned) != 3 {
		t.Fatalf("len(planned) = %d, want 3", len(planned))
	}
	if planned[0].ID != "call_2" {
		t.Errorf("planned[0].ID = %q, want %q", planned[0].ID, "call_2")
	}
	if planned[1].ID != "call_1" {
		t.Errorf("planned[1].ID = %q, want %q", planned[1].ID, "call_1")
	}
	if planned[2].ID != "run" {
		t.Errorf("planned[2].ID = %q, want %q", planned[2].ID, "run")
	}
}

func loadAllTestTools(t *testing.T) map[string]nacelle.Tool {
	set, err := tools.New(tools.Config{Root: t.TempDir(), AllowBash: true})
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	t.Cleanup(func() {
		if err := set.Close(); err != nil {
			t.Errorf("set.Close: %v", err)
		}
	})
	allTools, err := set.Tools()
	if err != nil {
		t.Fatalf("set.Tools: %v", err)
	}
	fetchTools, err := tools.WebFetch()
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	searchTools, err := tools.WebSearch("https://example.com")
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	allTools = append(append(allTools, fetchTools...), searchTools...)
	return nacelle.ToolsByName(allTools)
}

func TestPlanCallsIntegratesWithRealTools(t *testing.T) {
	byName := loadAllTestTools(t)
	calls := []nacelle.Invocation{
		{ID: "c1", Name: "write_file"},
		{ID: "c2", Name: "read_file"},
		{ID: "c3", Name: "edit_file"},
		{ID: "c4", Name: "list_directory"},
		{ID: "c5", Name: "run_command"},
		{ID: "c6", Name: "find_files"},
		{ID: "c7", Name: "web_fetch"},
		{ID: "c8", Name: "search_content"},
		{ID: "c9", Name: "web_search"},
	}

	planned := nacelle.PlanCalls(calls, byName)
	if len(planned) != len(calls) {
		t.Fatalf("len(planned) = %d, want %d", len(planned), len(calls))
	}

	for i := 0; i < 6; i++ {
		tool := byName[planned[i].Name]
		if ro, ok := tool.(nacelle.ReadOnlyTool); !ok || !ro.IsReadOnly() {
			t.Errorf("call %d (%s) expected read-only", i, planned[i].Name)
		}
	}
	for i := 6; i < len(planned); i++ {
		tool := byName[planned[i].Name]
		if ro, ok := tool.(nacelle.ReadOnlyTool); ok && ro.IsReadOnly() {
			t.Errorf("call %d (%s) expected mutating", i, planned[i].Name)
		}
	}
}
