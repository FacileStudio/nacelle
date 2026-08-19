package openrouter

import (
	"context"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/mcp"
)

func TestReasoningIsForwardedOnlyWhenAskedFor(t *testing.T) {
	const withReasoning = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"thinking about it","content":""},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}

data: {"id":"g","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0.1}}

data: [DONE]

`
	backend, handler := serve(t, withReasoning)

	silent := collect(t, backend, nacelle.Request{System: "s"})
	if got := kinds(silent, nacelle.KindThinking); len(got) != 0 {
		t.Errorf("reasoning was streamed without being asked for: %+v", got)
	}
	if _, sent := handler.requests[0]["reasoning"]; sent {
		t.Error("a reasoning parameter was sent for a request that wanted none")
	}

	loud := collect(t, backend, nacelle.Request{System: "s", Thinking: true})
	thinking := kinds(loud, nacelle.KindThinking)
	if len(thinking) != 1 || thinking[0].Text != "thinking about it" {
		t.Errorf("thinking = %+v, want the reasoning delta", thinking)
	}
}

// Effort without Thinking means think hard and show nothing: reasoning nobody
// reads still fills the context window.
func TestEffortAndExclusionReachTheRequest(t *testing.T) {
	backend, handler := serve(t, withKeepalive)
	collect(t, backend, nacelle.Request{System: "s", Effort: nacelle.EffortHigh})

	reasoning, ok := handler.requests[0]["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("no reasoning parameter: %+v", handler.requests[0])
	}
	if reasoning["effort"] != "high" {
		t.Errorf("effort = %v, want high", reasoning["effort"])
	}
	if reasoning["exclude"] != true {
		t.Errorf("exclude = %v, want true when the caller did not ask to see it", reasoning["exclude"])
	}
}

// Once the first token ships the status is committed, so a provider failure
// arrives in-band rather than as an HTTP error.
func TestAMidStreamErrorEndsTheRun(t *testing.T) {
	const withError = `data: {"id":"g","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}

data: {"id":"g","error":{"code":429,"message":"Rate limit exceeded","metadata":{"error_type":"rate_limit_exceeded"}},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}

data: [DONE]

`
	backend, _ := serve(t, withError)

	var failed error
	for _, err := range backend.Stream(context.Background(), nacelle.Request{System: "s"}) {
		if err != nil {
			failed = err
			break
		}
	}
	if failed == nil {
		t.Fatal("a mid-stream error was swallowed")
	}
	if !strings.Contains(failed.Error(), "Rate limit") {
		t.Errorf("error = %v, want the provider's message", failed)
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{APIKey: "k"}); err == nil {
		t.Error("a backend with no model was accepted")
	}
	t.Setenv("OPENROUTER_API_KEY", "")
	if _, err := New(Config{Model: "m"}); err == nil {
		t.Error("a backend with no API key was accepted")
	}
}

// MCP is the capability this backend cannot have, and saying so is the point:
// an agent that needs it fails at construction instead of running without it.
func TestMCPIsRefusedAtConstruction(t *testing.T) {
	backend, _ := serve(t, withKeepalive)

	_, err := nacelle.New(nacelle.Config{
		Backend: backend,
		System:  "s",
		MCP:     []mcp.Server{{Name: "p", URL: "https://p.test"}},
	})
	if err == nil {
		t.Fatal("an agent asking for MCP was built on a backend without it")
	}
	if !strings.Contains(err.Error(), "MCP") {
		t.Errorf("error = %v, want it to name MCP", err)
	}
}
