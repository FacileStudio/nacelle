package google_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/google"
)

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := google.New(google.Config{})
	if err == nil {
		t.Fatalf("expected error without API key, got nil")
	}
}

func TestNewWithGeminiAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-gemini-key")
	t.Setenv("GOOGLE_API_KEY", "")
	backend, err := google.New(google.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.Name() != "google" {
		t.Errorf("got name %q, want %q", backend.Name(), "google")
	}
	if backend.Model() != google.DefaultModel {
		t.Errorf("got model %q, want %q", backend.Model(), google.DefaultModel)
	}
}

func TestNewWithGoogleAPIKeyFallback(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "test-google-key")
	backend, err := google.New(google.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.Name() != "google" {
		t.Errorf("got name %q, want %q", backend.Name(), "google")
	}
	caps := backend.Capabilities()
	if caps.MCP || caps.Cost || caps.TokenCounting || !caps.Thinking || !caps.Effort {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}

func TestBackendCountTokens(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	backend, err := google.New(google.Config{})
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
			`data: {"choices":[{"delta":{"content":"Google"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"delta":{"content":" Gemini"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
		}
	}))
	defer server.Close()

	backend, err := google.New(google.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gemini-3.7-flash",
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
			if event.Usage.InputTokens != 12 || event.Usage.OutputTokens != 6 {
				t.Errorf("got usage %+v, want 12/6", event.Usage)
			}
		}
	}
	if text != "Google Gemini" {
		t.Errorf("got text %q, want %q", text, "Google Gemini")
	}
	if !done {
		t.Errorf("expected KindDone event")
	}
}

type testGoogleTool struct{}

func (testGoogleTool) Name() string        { return "calc" }
func (testGoogleTool) Description() string { return "calculate" }
func (testGoogleTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expr": map[string]any{"type": "string"},
		},
	}
}
func (testGoogleTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	return "42", nil
}

func TestBackendStreamToolCalling(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			chunks := []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_g1","type":"function","function":{"name":"calc","arguments":"{\"expr\":\"6*7\"}"}}]}}],"usage":null}` + "\n\n",
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
				`data: [DONE]` + "\n\n",
			}
			for _, c := range chunks {
				fmt.Fprint(w, c)
			}
			return
		}
		chunks := []string{
			`data: {"choices":[{"delta":{"content":"Result is 42"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
		}
	}))
	defer server.Close()

	backend, err := google.New(google.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gemini-3.7-flash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawToolCall, sawToolResult bool
	var text string
	for event, err := range backend.Stream(context.Background(), nacelle.Request{
		Tools: []nacelle.Tool{testGoogleTool{}},
	}) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		switch event.Kind {
		case nacelle.KindToolCall:
			sawToolCall = true
		case nacelle.KindToolResult:
			sawToolResult = true
			if event.Tool.Result != "42" {
				t.Errorf("got tool result %q, want %q", event.Tool.Result, "42")
			}
		case nacelle.KindText:
			text += event.Text
		}
	}
	if !sawToolCall || !sawToolResult {
		t.Errorf("expected tool call and result, got call=%v result=%v", sawToolCall, sawToolResult)
	}
	if text != "Result is 42" {
		t.Errorf("got text %q, want %q", text, "Result is 42")
	}
}
