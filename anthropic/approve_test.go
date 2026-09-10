package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// A refusal is enforced in one place, RunTool, which means a refused call must
// never reach the tool body and must still surface as a KindToolResult carrying
// an error — the pairing contract a consumer builds on.
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

	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"search","input":{}}}`,
			arguments(t, 0, `{"query":"x"}`),
			`{"type":"content_block_stop","index":0}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
		sse(t, messageStart(), messageDelta("end_turn"), `{"type":"message_stop"}`),
	)})

	deny := func(context.Context, string, json.RawMessage) bool { return false }
	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{tool},
		Approve:       deny,
		MaxTokens:     1024,
		MaxIterations: 4,
	})

	if ran {
		t.Fatal("the tool ran despite being refused")
	}
	result := toolsOf(events, nacelle.KindToolResult)["toolu_1"]
	if result == nil || result.Err == nil {
		t.Fatalf("saw result %+v, want a refused result carrying an error", result)
	}
	if !result.Refused {
		t.Errorf("result %+v was not marked Refused", result)
	}
}

// The ordinary case is unchanged by adding Approve: nil runs every call the
// way this package always has, and a real approve function that says yes has
// to let the call through, not just fail to crash.
func TestAnApprovedCallReachesTheTool(t *testing.T) {
	tool, err := nacelle.NewTool("search", "Find things", func(context.Context, struct {
		Query string `json:"query" jsonschema:"required"`
	}) (string, error) {
		return "found it", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"search","input":{}}}`,
			arguments(t, 0, `{"query":"x"}`),
			`{"type":"content_block_stop","index":0}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
		sse(t, messageStart(), messageDelta("end_turn"), `{"type":"message_stop"}`),
	)})

	allow := func(context.Context, string, json.RawMessage) bool { return true }
	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{tool},
		Approve:       allow,
		MaxTokens:     1024,
		MaxIterations: 4,
	})

	result := toolsOf(events, nacelle.KindToolResult)["toolu_1"]
	if result == nil || result.Err != nil {
		t.Fatalf("saw result %+v, want an approved result with no error", result)
	}
	if result.Result != "found it" {
		t.Errorf("result = %q, want the tool's own answer", result.Result)
	}
}
