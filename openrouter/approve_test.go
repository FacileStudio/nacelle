package openrouter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"

	"github.com/openai/openai-go/v3"
)

// A refusal has to reach the model as a normal tool message, not abort the
// run — runCall never returns an error itself, so this backend's own loop
// keeps going the same way it does for any other tool failure.
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

	deny := func(context.Context, string, json.RawMessage) bool { return false }
	call := callContext{
		byName: nacelle.ToolsByName([]nacelle.Tool{tool}),
		sink:   &nacelle.ToolSink{Approve: deny},
	}

	message := runCall(context.Background(), toolCall{id: "c1", name: "search", arguments: `{"query":"x"}`}, 0, call)

	if ran {
		t.Fatal("the tool ran despite being refused")
	}
	if !strings.Contains(messageText(t, message), "not approved") {
		t.Errorf("message = %+v, want the model told this call was not approved", message)
	}
}

func TestAnApprovedCallReachesTheTool(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, struct {
		Query string `json:"query" jsonschema:"required"`
	}) (string, error) {
		return "found it", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	allow := func(context.Context, string, json.RawMessage) bool { return true }
	call := callContext{
		byName: nacelle.ToolsByName([]nacelle.Tool{tool}),
		sink:   &nacelle.ToolSink{Approve: allow},
	}

	message := runCall(context.Background(), toolCall{id: "c1", name: "search", arguments: `{"query":"x"}`}, 0, call)

	if got := messageText(t, message); got != "found it" {
		t.Errorf("message text = %q, want the tool's own result", got)
	}
}

// messageText reads the plain string content runCall always sends — every
// path through it calls openai.ToolMessage with a string, never the
// content-part-array form.
func messageText(t *testing.T, message openai.ChatCompletionMessageParamUnion) string {
	t.Helper()
	if message.OfTool == nil {
		t.Fatal("message is not a tool message")
	}
	return message.OfTool.Content.OfString.Or("")
}
