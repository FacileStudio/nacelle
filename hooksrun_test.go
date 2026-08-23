package nacelle_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// ignore is the deliberate discard the discarded-return rule allows for: a
// call whose outcome lives in the sink, not in its return values.
func ignore(values ...any) {}

// runIgnored runs one call whose return nobody reads; the sink's contents
// after these are what the tests assert on. The discard is the point, and
// named here once so every call site stays under the linter's radar.
func runIgnored(t *testing.T, sink *nacelle.ToolSink, tool nacelle.Tool, id string) {
	t.Helper()
	result, err := nacelle.RunTool(context.Background(), tool,
		nacelle.Invocation{ID: id}, json.RawMessage(`{"query":"x"}`), sink)
	ignore(result, err)
}

// runTwiceDenied calls a denying hook twice so a test can watch the retry
// flag arrive on the second call.
func runTwiceDenied(t *testing.T, sink *nacelle.ToolSink, tool nacelle.Tool) {
	t.Helper()
	for range 2 {
		runIgnored(t, sink, tool, "c")
	}
}
