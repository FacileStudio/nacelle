package openrouter

import (
	"context"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// OpenRouter's own sixth finish reason, arriving without the top-level error
// object that usually accompanies it. Nothing aborts the stream, so the run
// completes and the only sign the generation failed is this string.
const failedGeneration = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","content":"partial"},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"error"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4,"cost":0.00001}}

data: [DONE]

`

// A generation that failed part-way is not a finished answer, and OpenRouter
// says so in a value the OpenAI schema does not document. Left unlisted it
// still lands on StopOther by default, which is right by accident: this pins
// it so the next person to touch the mapping cannot make "error" mean StopEnd.
func TestAFailedGenerationDoesNotLookLikeAFinishedRun(t *testing.T) {
	if got := stopOf("error"); got != nacelle.StopOther {
		t.Errorf("stopOf(\"error\") = %q, want %q", got, nacelle.StopOther)
	}

	backend, _ := serve(t, failedGeneration)
	events := collect(t, backend, nacelle.Request{System: "s"})

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Stop.Complete() {
		t.Fatalf("done = %+v, want one run a consumer cannot mistake for a finished answer", done)
	}
	if done[0].Usage.Total() == 0 {
		t.Error("the tokens spent on the failed generation were dropped; they were still billed")
	}
}

// named builds a tool that does nothing, for a test that only reads the schema.
func named(t *testing.T, name string) nacelle.Tool {
	t.Helper()
	tool, err := nacelle.NewTool(name, "Find things", func(context.Context, searchInput) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return tool
}

// Prompt caching keys on an exact prefix and the tool schema sits at the front
// of every request, so a caller whose tool order varies between runs — one
// built from a map, say — pays full price for a prompt that never changed.
func TestToolsAreSentInAStableOrder(t *testing.T) {
	backend, handler := serve(t, withKeepalive)
	tools := []nacelle.Tool{named(t, "zebra"), named(t, "alpha"), named(t, "mango")}
	collect(t, backend, nacelle.Request{System: "s", Tools: tools})

	sent, _ := handler.requests[0]["tools"].([]any)
	if len(sent) != 3 {
		t.Fatalf("sent %d tools, want 3", len(sent))
	}

	var names []string
	for _, entry := range sent {
		tool, _ := entry.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		names = append(names, name)
	}
	if names[0] != "alpha" || names[1] != "mango" || names[2] != "zebra" {
		t.Errorf("tools = %v, want them sorted by name so the cache prefix is stable", names)
	}
	if tools[0].Name() != "zebra" {
		t.Errorf("the caller's slice was reordered to %q; it belongs to them", tools[0].Name())
	}
}
