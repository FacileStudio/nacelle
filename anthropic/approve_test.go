package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// A refusal has to reach the model the same way any other tool failure
// does: Execute returns an error, which the SDK's own runner turns into a
// tool_result marked as an error rather than aborting the run — see
// executeToolUse in the vendor SDK. This only has to prove the refusal
// reaches Execute at all; the SDK's own handling of that error is not this
// package's to test.
func TestARefusedCallNeverReachesTheTool(t *testing.T) {
	ran := false
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, struct {
		Query string `json:"query" jsonschema:"required"`
	}) (string, error) {
		ran = true
		return "should never happen", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	pending := newInvocations()
	pending.reset([]*nacelle.ToolEvent{{ID: "toolu_1", Name: "search", Input: `{"query":"x"}`}})

	deny := func(context.Context, string, json.RawMessage) bool { return false }
	adapted := adapt([]nacelle.Tool{tool}, &nacelle.ToolSink{Approve: deny}, pending)

	if _, err := adapted[0].Execute(context.Background(), []byte(`{"query":"x"}`)); err == nil {
		t.Fatal("Execute returned no error for a refused call")
	}
	if ran {
		t.Fatal("the tool ran despite being refused")
	}
}

// The ordinary case is unchanged by adding Approve: nil runs every call the
// way this package always has, and a real approve function that says yes
// has to let the call through, not just fail to crash.
func TestAnApprovedCallReachesTheTool(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, struct {
		Query string `json:"query" jsonschema:"required"`
	}) (string, error) {
		return "found it", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	pending := newInvocations()
	pending.reset([]*nacelle.ToolEvent{{ID: "toolu_1", Name: "search", Input: `{"query":"x"}`}})

	allow := func(context.Context, string, json.RawMessage) bool { return true }
	adapted := adapt([]nacelle.Tool{tool}, &nacelle.ToolSink{Approve: allow}, pending)

	content, err := adapted[0].Execute(context.Background(), []byte(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	text, ok := sdkTextBlock(t, content)
	if !ok || text != "found it" {
		t.Errorf("content = %+v, want the tool's own result", content)
	}
}

// sdkTextBlock reads the text out of the one content block Execute returns,
// so the test above can assert on what the model would actually be told.
func sdkTextBlock(t *testing.T, content []sdk.BetaToolResultBlockParamContentUnion) (string, bool) {
	t.Helper()
	if len(content) != 1 || content[0].OfText == nil {
		return "", false
	}
	return content[0].OfText.Text, true
}
