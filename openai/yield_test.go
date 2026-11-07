package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/openai"
)

// A tool returning EndTurn ends the run after its round, on the OpenAI loop
// too: the other call still runs and no second request is made.
func TestEndTurnYieldsAfterTheRound(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"ask","arguments":"{}"}},{"index":1,"id":"call_r","type":"function","function":{"name":"read_op","arguments":"{}"}}]}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}))
	t.Cleanup(server.Close)

	backend, err := openai.New(openai.Config{APIKey: "test-key", BaseURL: server.URL, Model: "gpt-5.4"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ask, err := nacelle.NewTool("ask", "Ask the user", func(context.Context, struct{}) (string, error) {
		return "", nacelle.EndTurn("waiting")
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	ran := false
	read := testPlannedTool{name: "read_op", readOnly: true, onRun: func() { ran = true }}

	var last nacelle.Event
	var askResult *nacelle.ToolEvent
	for ev, err := range backend.Stream(context.Background(), nacelle.Request{Tools: []nacelle.Tool{ask, read}}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if ev.Kind == nacelle.KindToolResult && ev.Tool.ID == "call_a" {
			askResult = ev.Tool
		}
		last = ev
	}
	if askResult == nil || askResult.Err != nil || askResult.Result != "waiting" {
		t.Fatalf("ask result = %+v, want a success carrying the EndTurn result", askResult)
	}
	if !ran || requests != 1 {
		t.Fatalf("read ran %v after %d requests, want it run after one", ran, requests)
	}
	if last.Kind != nacelle.KindDone || last.Stop != nacelle.StopYield {
		t.Fatalf("last event = %+v, want KindDone with StopYield", last)
	}
}
