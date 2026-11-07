package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/openai"
)

// yieldingServer answers every request with one round of two tool calls,
// ask then read_op, and counts the requests it served.
func yieldingServer(t *testing.T, requests *int) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*requests++
		writeChunks(w, []string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"ask","arguments":"{}"}},{"index":1,"id":"call_r","type":"function","function":{"name":"read_op","arguments":"{}"}}]}}],"usage":null}` + "\n\n",
			`data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n",
			`data: [DONE]` + "\n\n",
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// A tool returning EndTurn ends the run after its round, on the OpenAI loop
// too: the other call still runs and no second request is made.
func TestEndTurnYieldsAfterTheRound(t *testing.T) {
	requests := 0
	backend, err := openai.New(openai.Config{APIKey: "test-key", BaseURL: yieldingServer(t, &requests).URL, Model: "gpt-5.4"})
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

	var events []nacelle.Event
	for ev, err := range backend.Stream(context.Background(), nacelle.Request{Tools: []nacelle.Tool{ask, read}}) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		events = append(events, ev)
	}
	if !ran || requests != 1 {
		t.Fatalf("read ran %v after %d requests, want it run after one", ran, requests)
	}
	assertYielded(t, events)
}

// assertYielded checks that the ask call closed as a success carrying the
// EndTurn result and that the run ended with StopYield.
func assertYielded(t *testing.T, events []nacelle.Event) {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == nacelle.KindToolResult && ev.Tool.ID == "call_a" && (ev.Tool.Err != nil || ev.Tool.Result != "waiting") {
			t.Fatalf("ask result = %+v, want a success carrying the EndTurn result", ev.Tool)
		}
	}
	if last := events[len(events)-1]; last.Kind != nacelle.KindDone || last.Stop != nacelle.StopYield {
		t.Fatalf("last event = %+v, want KindDone with StopYield", last)
	}
}
