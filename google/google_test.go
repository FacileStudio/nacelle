package google_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/google"
	"iter"
)

func collectStream(t *testing.T, stream iter.Seq2[nacelle.Event, error]) (string, bool, nacelle.Usage) {
	t.Helper()
	var (
		text  strings.Builder
		done  bool
		usage nacelle.Usage
	)
	for ev, err := range stream {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		switch ev.Kind {
		case nacelle.KindText:
			text.WriteString(ev.Text)
		case nacelle.KindTurn:
			done = true
			usage = ev.Usage
		case nacelle.KindDone:
			done = true
			usage = ev.Usage
		}
	}
	return text.String(), done, usage
}

// collectToolStream collects text, tool calls/results, and final usage from a stream.
func collectToolStream(t *testing.T, stream iter.Seq2[nacelle.Event, error]) struct {
	text          string
	sawToolCall   bool
	sawToolResult bool
	toolResult    string
	usage         nacelle.Usage
} {
	t.Helper()
	var out struct {
		text          string
		sawToolCall   bool
		sawToolResult bool
		toolResult    string
		usage         nacelle.Usage
	}
	for ev, err := range stream {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		switch ev.Kind {
		case nacelle.KindText:
			out.text += ev.Text
		case nacelle.KindToolCall:
			out.sawToolCall = true
		case nacelle.KindToolResult:
			out.sawToolResult = true
			if ev.Tool != nil {
				out.toolResult = ev.Tool.Result
			}
		case nacelle.KindTurn:
			out.usage = ev.Usage
		case nacelle.KindDone:
			out.usage = ev.Usage
		}
	}
	return out
}


func writeChunks(w http.ResponseWriter, chunks []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprint(w, c)
	}
}

// toolHandler answers the first request with a tool call, the second with
// text. It is the shared body of the tool-calling tests.
func toolHandler(calls *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if *calls == 1 {
			writeChunks(w, []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_g1","type":"function","function":{"name":"calc","arguments":"{\"expr\":\"6*7\"}"}}]}}],"usage":null}` + "\n\n",
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
				`data: [DONE]` + "\n\n",
			})
			return
		}
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"content":"Result is 42"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}
}

// toolBackend returns a backend backed by a server whose first answer is a
// tool call and second is text, with the call counter shared so the test can
// inspect it.
func toolBackend(t *testing.T) (*google.Backend, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(toolHandler(&calls))
	t.Cleanup(server.Close)
	backend, err := google.New(google.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gemini-3.7-flash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return backend, &calls
}

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
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"content":"Google"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"delta":{"content":" Gemini"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
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

	text, done, usage := collectStream(t, backend.Stream(context.Background(), nacelle.Request{System: "test"}))
	if text != "Google Gemini" {
		t.Errorf("got text %q, want %q", text, "Google Gemini")
	}
	if !done {
		t.Errorf("expected KindDone event")
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 6 {
		t.Errorf("got usage %+v, want 12/6", usage)
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
	backend, _ := toolBackend(t)
	out := collectToolStream(t, backend.Stream(context.Background(), nacelle.Request{
		Tools: []nacelle.Tool{testGoogleTool{}},
	}))
	if !out.sawToolCall || !out.sawToolResult {
		t.Errorf("expected tool call and result, got call=%v result=%v", out.sawToolCall, out.sawToolResult)
	}
	if out.toolResult != "42" {
		t.Errorf("got tool result %q, want %q", out.toolResult, "42")
	}
	if out.text != "Result is 42" {
		t.Errorf("got text %q, want %q", out.text, "Result is 42")
	}
}