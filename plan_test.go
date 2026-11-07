package nacelle_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/tools"
)

// rendezvous is two read-only tools that each wait for the other, so they can
// only both finish when the loop runs them at the same time, and a write tool
// that records whether both reads had finished when it ran.
type rendezvous struct {
	mu        sync.Mutex
	arrived   chan struct{}
	reads     int
	readsSeen int
}

func (r *rendezvous) read(ctx context.Context, _ struct{}) (string, error) {
	select {
	case r.arrived <- struct{}{}:
	case <-r.arrived:
	case <-time.After(5 * time.Second):
		return "", errors.New("the other read never ran beside this one")
	}
	r.mu.Lock()
	r.reads++
	r.mu.Unlock()
	return "read", nil
}

func (r *rendezvous) write(context.Context, struct{}) (string, error) {
	r.mu.Lock()
	r.readsSeen = r.reads
	r.mu.Unlock()
	return "wrote", nil
}

func (r *rendezvous) tools(t *testing.T) []nacelle.Tool {
	t.Helper()
	readOnly := nacelle.ToolOptions{ReadOnly: true}
	first, err := nacelle.NewToolWithOptions("read_a", "Read", r.read, readOnly)
	if err != nil {
		t.Fatalf("NewToolWithOptions: %v", err)
	}
	second, err := nacelle.NewToolWithOptions("read_b", "Read", r.read, readOnly)
	if err != nil {
		t.Fatalf("NewToolWithOptions: %v", err)
	}
	write, err := nacelle.NewTool("write", "Write", r.write)
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return []nacelle.Tool{first, second, write}
}

func call(id, name string) nacelle.ToolCall {
	return nacelle.ToolCall{ID: id, Name: name, Input: json.RawMessage(`{}`), Finished: true}
}

// The model asked for the write first; the loop still runs both reads first,
// together, and the write after them, and answers in the model's order.
func TestReadOnlyCallsRunFirstAndTogether(t *testing.T) {
	r := &rendezvous{arrived: make(chan struct{})}
	available := r.tools(t)
	got := runSteps(t, context.Background(), nacelle.Request{Tools: available}, available[0],
		roundStep(call("w", "write"), call("a", "read_a"), call("b", "read_b")), textStep("done"))

	for _, event := range got.toolEvents() {
		if event.Tool.Err != nil {
			t.Fatalf("%s failed: %v", event.Tool.Name, event.Tool.Err)
		}
	}
	if r.readsSeen != 2 {
		t.Errorf("the write saw %d reads finished, want both", r.readsSeen)
	}
	results := lastResults(got.events)
	if len(results) != 3 || results[0].ID != "w" || results[1].ID != "a" || results[2].ID != "b" {
		t.Errorf("results = %+v, want them in the order the model asked", results)
	}
}

// lastResults are the tool results of the last result message committed.
func lastResults(events []nacelle.Event) []nacelle.ToolResult {
	var results []nacelle.ToolResult
	for _, event := range events {
		if event.Kind != nacelle.KindMessage || event.Message.Role != nacelle.RoleUser {
			continue
		}
		results = results[:0]
		for _, part := range event.Message.Parts {
			if result, ok := part.(nacelle.ToolResult); ok {
				results = append(results, result)
			}
		}
	}
	return results
}

func loadAllTestTools(t *testing.T) map[string]nacelle.Tool {
	set, err := tools.New(tools.Config{Root: t.TempDir(), AllowBash: true})
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	t.Cleanup(func() {
		set.Close()
	})
	allTools, err := set.Tools()
	if err != nil {
		t.Fatalf("set.Tools: %v", err)
	}
	fetchTools, err := tools.WebFetch()
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	allTools = append(allTools, fetchTools...)
	return nacelle.ToolsByName(allTools)
}

// The built-in tools that only read say so, and the ones that write do not.
func TestBuiltInToolsDeclareWhetherTheyOnlyRead(t *testing.T) {
	byName := loadAllTestTools(t)
	wanted := map[string]bool{
		"read_file": true, "list_directory": true, "glob": true, "grep": true, "web_fetch": true,
		"write_file": false, "edit_file": false, "bash": false,
	}
	for name, readOnly := range wanted {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("no built-in tool is named %q", name)
			continue
		}
		ro, declared := tool.(nacelle.ReadOnlyTool)
		if got := declared && ro.IsReadOnly(); got != readOnly {
			t.Errorf("%s read-only = %v, want %v", name, got, readOnly)
		}
	}
}

func TestLegacyToolNamesPointAtTheBuiltInTools(t *testing.T) {
	byName := loadAllTestTools(t)
	for old, current := range nacelle.LegacyToolNames() {
		if _, ok := byName[current]; !ok && current != "todo_write" {
			t.Errorf("LegacyToolNames[%q] = %q, which no built-in tool is named", old, current)
		}
	}
}
