package openrouter

import (
	"context"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/mcp"
)

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{APIKey: "k"}); err == nil {
		t.Error("a backend with no model was accepted")
	}
	t.Setenv("OPENROUTER_API_KEY", "")
	if _, err := New(Config{Model: "m"}); err == nil {
		t.Error("a backend with no API key was accepted")
	}
}

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
