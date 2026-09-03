package google_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/google"
)

type testPlannedTool struct {
	name     string
	readOnly bool
	onRun    func()
}

func (t testPlannedTool) Name() string           { return t.name }
func (t testPlannedTool) Description() string    { return t.name }
func (t testPlannedTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t testPlannedTool) IsReadOnly() bool       { return t.readOnly }
func (t testPlannedTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	if t.onRun != nil {
		t.onRun()
	}
	return t.name + " ok", nil
}

func TestBackendStreamToolPlanning(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeChunks(w, []string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_w","type":"function","function":{"name":"write_op","arguments":"{}"}},{"index":1,"id":"call_r","type":"function","function":{"name":"read_op","arguments":"{}"}}]}}],"usage":null}` + "\n\n",
				`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
				`data: [DONE]` + "\n\n",
			})
			return
		}
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"content":"All done"}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":15,"completion_tokens":4,"total_tokens":19}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}))
	t.Cleanup(server.Close)

	backend, err := google.New(google.Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gemini-3.7-flash",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var executionOrder []string
	readTool := testPlannedTool{
		name:     "read_op",
		readOnly: true,
		onRun:    func() { executionOrder = append(executionOrder, "read_op") },
	}
	writeTool := testPlannedTool{
		name:     "write_op",
		readOnly: false,
		onRun:    func() { executionOrder = append(executionOrder, "write_op") },
	}

	out := collectToolStream(t, backend.Stream(context.Background(), nacelle.Request{
		Tools: []nacelle.Tool{writeTool, readTool},
	}))
	if out.text != "All done" {
		t.Errorf("got text %q, want %q", out.text, "All done")
	}
	if len(executionOrder) != 2 {
		t.Fatalf("executed %d tools, want 2", len(executionOrder))
	}
	if executionOrder[0] != "read_op" {
		t.Errorf("executionOrder[0] = %q, want %q", executionOrder[0], "read_op")
	}
	if executionOrder[1] != "write_op" {
		t.Errorf("executionOrder[1] = %q, want %q", executionOrder[1], "write_op")
	}
}
