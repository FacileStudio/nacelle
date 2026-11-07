package client

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/FacileStudio/nacelle"
)

// Catalog mounts tools behind three tools of its own once there are more than
// threshold of them: search_tools to find one, get_tool_details to read its
// schema, and call_tool to run it. At or under the threshold it returns tools
// unchanged, because a catalog costs the model two extra calls per tool.
//
// call_tool is a nacelle.Router, so the loop runs the named tool itself:
// hooks, Approve and the result event see that tool's name and input, never
// call_tool's, and the tool's own read-only declaration and images hold.
func Catalog(tools []nacelle.Tool, threshold int) []nacelle.Tool {
	if len(tools) <= threshold {
		return tools
	}
	c := &catalog{tools: slices.SortedFunc(slices.Values(tools), func(a, b nacelle.Tool) int {
		return cmp.Compare(a.Spec().Name, b.Spec().Name)
	})}
	return []nacelle.Tool{catalogSearch{c}, catalogDetails{c}, catalogCall{c}}
}

// catalog is the tools behind a Catalog, sorted by name.
type catalog struct{ tools []nacelle.Tool }

// find returns the tool with this name, or the one whose name ends in
// "_"+name, which is how a model shortens a bridged tool's server prefix.
func (c *catalog) find(name string) (nacelle.Tool, bool) {
	for _, tool := range c.tools {
		if tool.Spec().Name == name {
			return tool, true
		}
	}
	for _, tool := range c.tools {
		if strings.HasSuffix(tool.Spec().Name, "_"+name) {
			return tool, true
		}
	}
	return nil, false
}

// search returns the tools whose name or description contains query, ignoring
// case; an empty query matches every one.
func (c *catalog) search(query string) []nacelle.Spec {
	query = strings.ToLower(query)
	var hits []nacelle.Spec
	for _, tool := range c.tools {
		spec := tool.Spec()
		if strings.Contains(strings.ToLower(spec.Name+" "+spec.Description), query) {
			hits = append(hits, spec)
		}
	}
	return hits
}

// catalogSearch is the catalog's search_tools.
type catalogSearch struct{ catalog *catalog }

// Spec describes search_tools.
func (catalogSearch) Spec() nacelle.Spec {
	return nacelle.Spec{
		Name: "search_tools",
		Description: "Search the available tools by name or description. Call this before assuming a tool " +
			"exists; an empty query lists every one. Returns name and one-line description pairs, not " +
			"input schemas — get_tool_details reads those.",
		Schema: map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Words from the job: name fragments or capabilities. Empty or omitted lists everything."},
		}},
		ReadOnly: true,
	}
}

// Run lists the tools matching the query.
func (t catalogSearch) Run(_ context.Context, call nacelle.Call, _ func(string)) (nacelle.Result, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := decode(call.Input, &in); err != nil {
		return nacelle.Result{}, fmt.Errorf("reading query: %w", err)
	}
	hits := t.catalog.search(in.Query)
	if len(hits) == 0 {
		return nacelle.Result{Text: fmt.Sprintf("No tool matches %q. Fewer or broader words find more.", in.Query)}, nil
	}
	var body strings.Builder
	for _, hit := range hits {
		fmt.Fprintf(&body, "%s — %s\n", hit.Name, hit.Description)
	}
	return nacelle.Result{Text: body.String()}, nil
}

// decode reads a call's input, treating no input as an empty object.
func decode(input json.RawMessage, into any) error {
	if len(input) == 0 {
		return nil
	}
	return json.Unmarshal(input, into)
}
