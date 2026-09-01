package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/openai"
)

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := openai.New(openai.Config{})
	if err == nil {
		t.Fatalf("expected error without API key, got nil")
	}
}

func TestNewWithDefaults(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	backend, err := openai.New(openai.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.Name() != "openai" {
		t.Errorf("got name %q, want %q", backend.Name(), "openai")
	}
	if backend.Model() != openai.DefaultModel {
		t.Errorf("got model %q, want %q", backend.Model(), openai.DefaultModel)
	}
	caps := backend.Capabilities()
	if caps.MCP || caps.Cost || caps.TokenCounting || !caps.Thinking || !caps.Effort {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}

func TestBackendCountTokens(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	backend, err := openai.New(openai.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = backend.CountTokens(context.Background(), nacelle.Request{})
	if err == nil {
		t.Fatalf("expected unsupported error, got nil")
	}
}

func TestBackendStreamText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`data: {"choices":[{"delta":{"content":"Hello"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"delta":{"content":" world"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
		}
	}))
	defer server.Close()

	backend, err := openai.New(openai.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	var done bool
	for event, err := range backend.Stream(context.Background(), nacelle.Request{System: "test"}) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		switch event.Kind {
		case nacelle.KindText:
			text += event.Text
		case nacelle.KindDone:
			done = true
			if event.Usage.InputTokens != 10 || event.Usage.OutputTokens != 5 {
				t.Errorf("got usage %+v, want 10/5", event.Usage)
			}
		}
	}
	if text != "Hello world" {
		t.Errorf("got text %q, want %q", text, "Hello world")
	}
	if !done {
		t.Errorf("expected KindDone event")
	}
}

type testEchoTool struct{}

func (testEchoTool) Name() string        { return "echo" }
func (testEchoTool) Description() string { return "echo input" }
func (testEchoTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
	}
}
func (testEchoTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var payload struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", err
	}
	return "echo: " + payload.Msg, nil
}

func TestBackendStreamToolCalling(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			chunks := []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"msg\":\"hi\"}"}}]}}],"usage":null}` + "\n\n",
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
				`data: [DONE]` + "\n\n",
			}
			for _, c := range chunks {
				fmt.Fprint(w, c)
			}
			return
		}
		chunks := []string{
			`data: {"choices":[{"delta":{"content":"Done with tool"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":15,"completion_tokens":4,"total_tokens":19}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
		}
	}))
	defer server.Close()

	backend, err := openai.New(openai.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawToolCall, sawToolResult bool
	var text string
	for event, err := range backend.Stream(context.Background(), nacelle.Request{
		Tools: []nacelle.Tool{testEchoTool{}},
	}) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		switch event.Kind {
		case nacelle.KindToolCall:
			sawToolCall = true
		case nacelle.KindToolResult:
			sawToolResult = true
			if event.Tool.Result != "echo: hi" {
				t.Errorf("got tool result %q, want %q", event.Tool.Result, "echo: hi")
			}
		case nacelle.KindText:
			text += event.Text
		}
	}
	if !sawToolCall || !sawToolResult {
		t.Errorf("expected tool call and result, got call=%v result=%v", sawToolCall, sawToolResult)
	}
	if text != "Done with tool" {
		t.Errorf("got text %q, want %q", text, "Done with tool")
	}
}
