package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// heard is a tool that answers with the raw input it read.
type heard string

func (h heard) Spec() nacelle.Spec {
	return nacelle.Spec{Name: string(h), Description: "Echo " + string(h), ReadOnly: true}
}

func (h heard) Run(_ context.Context, call nacelle.Call, _ func(string)) (nacelle.Result, error) {
	return nacelle.Result{Text: "heard " + string(call.Input)}, nil
}

// mounted builds a catalog over count tools named srv_tool_NN.
func mounted(t *testing.T, count int) []nacelle.Tool {
	t.Helper()
	tools := make([]nacelle.Tool, 0, count)
	for i := range count {
		tools = append(tools, heard(fmt.Sprintf("srv_tool_%02d", i)))
	}
	return Catalog(tools, count-1)
}

func TestASmallSetIsMountedWhole(t *testing.T) {
	tools := []nacelle.Tool{heard("srv_tool_00")}
	if got := Catalog(tools, 1); len(got) != 1 || got[0].Spec().Name != "srv_tool_00" {
		t.Fatalf("Catalog = %v, want the tool itself", names(got))
	}
}

func TestAGrownSetIsMountedAsACatalog(t *testing.T) {
	got := names(mounted(t, 3))
	if strings.Join(got, ",") != "search_tools,get_tool_details,call_tool" {
		t.Fatalf("Catalog = %v, want the three catalog tools", got)
	}
}

func TestSearchListsEverythingOnAnEmptyQueryAndFilters(t *testing.T) {
	search := find(t, mounted(t, 3), "search_tools")
	all, err := callTool(t.Context(), search, nil)
	if err != nil || strings.Count(all, "\n") != 3 {
		t.Fatalf("search_tools = %q, %v, want every tool", all, err)
	}
	one, _ := callTool(t.Context(), search, json.RawMessage(`{"query":"TOOL_01"}`))
	if strings.Count(one, "\n") != 1 {
		t.Errorf("search_tools = %q, want one match ignoring case", one)
	}
	none, _ := callTool(t.Context(), search, json.RawMessage(`{"query":"nothing"}`))
	if !strings.Contains(none, "No tool matches") {
		t.Errorf("search_tools = %q, want a miss said as one", none)
	}
}

func TestDetailsReturnTheSchemaAndMiss(t *testing.T) {
	details := find(t, mounted(t, 2), "get_tool_details")
	got, err := callTool(t.Context(), details, json.RawMessage(`{"name":"srv_tool_00"}`))
	if err != nil || !strings.Contains(got, "Input schema:") {
		t.Fatalf("get_tool_details = %q, %v, want the schema", got, err)
	}
	missed, _ := callTool(t.Context(), details, json.RawMessage(`{"name":"nope"}`))
	if !strings.Contains(missed, "No tool named nope") {
		t.Errorf("get_tool_details = %q, want a miss", missed)
	}
}

// call_tool routes to the named tool, by its suffix too, repairing the
// argument shapes models get wrong.
func TestCallToolRoutesToTheNamedTool(t *testing.T) {
	router, ok := find(t, mounted(t, 2), "call_tool").(nacelle.Router)
	if !ok {
		t.Fatal("call_tool is not a nacelle.Router")
	}
	for input, want := range map[string]string{
		`{"name":"srv_tool_01","arguments":{"a":1}}`: `{"a":1}`,
		`{"name":"tool_01","arguments":"{\"a\":1}"}`: `{"a":1}`,
		`{"name":"srv_tool_01"}`:                     `{}`,
		`{"name":"srv_tool_01","arguments":null}`:    `{}`,
	} {
		tool, args, err := router.Route(json.RawMessage(input))
		if err != nil || tool.Spec().Name != "srv_tool_01" || string(args) != want {
			t.Errorf("Route(%s) = %v, %s, %v; want srv_tool_01 with %s", input, tool, args, err, want)
		}
	}
	if _, _, err := router.Route(json.RawMessage(`{"name":"nope"}`)); err == nil {
		t.Error("a route to no tool succeeded")
	}
}

// Called directly rather than through the loop, call_tool still runs the tool.
func TestCallToolRunsOutsideTheLoop(t *testing.T) {
	got, err := callTool(t.Context(), find(t, mounted(t, 2), "call_tool"), json.RawMessage(`{"name":"srv_tool_00"}`))
	if err != nil || got != "heard {}" {
		t.Fatalf("call_tool = %q, %v, want the tool's answer", got, err)
	}
}
