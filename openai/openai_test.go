package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/openai"
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
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"msg\":\"hi\"}"}}]}}],"usage":null}` + "\n\n",
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
				`data: [DONE]` + "\n\n",
			})
			return
		}
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"content":"Done with tool"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":15,"completion_tokens":4,"total_tokens":19}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}
}

// toolBackend returns a backend backed by a server whose first answer is a
// tool call and second is text, with the call counter shared so the test can
// inspect it.
func toolBackend(t *testing.T) (*openai.Backend, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(toolHandler(&calls))
	t.Cleanup(server.Close)
	backend, err := openai.New(openai.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return backend, &calls
}

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := openai.New(openai.Config{})
	if err == nil {
		t.Fatalf("expected error without API key, got nil")
	}
}

func TestNewWithAPIKey(t *testing.T) {
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
}

func TestNewWithEmptyAPIKeyFallsBackToEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	backend, err := openai.New(openai.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.Name() != "openai" {
		t.Errorf("got name %q, want %q", backend.Name(), "openai")
	}
	caps := backend.Capabilities()
	if !caps.Thinking || !caps.Effort {
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
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"content":"Hello"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"delta":{"content":" world"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
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

	text, done, usage := collectStream(t, backend.Stream(context.Background(), nacelle.Request{System: "test"}))
	if text != "Hello world" {
		t.Errorf("got text %q, want %q", text, "Hello world")
	}
	if !done {
		t.Errorf("expected KindDone event")
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Errorf("got usage %+v, want 10/5", usage)
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
	backend, _ := toolBackend(t)
	out := collectToolStream(t, backend.Stream(context.Background(), nacelle.Request{
		Tools: []nacelle.Tool{testEchoTool{}},
	}))
	if !out.sawToolCall || !out.sawToolResult {
		t.Errorf("expected tool call and result, got call=%v result=%v", out.sawToolCall, out.sawToolResult)
	}
	if out.toolResult != "echo: hi" {
		t.Errorf("got tool result %q, want %q", out.toolResult, "echo: hi")
	}
	if out.text != "Done with tool" {
		t.Errorf("got text %q, want %q", out.text, "Done with tool")
	}
}