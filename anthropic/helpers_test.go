package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// stub serves one scripted turn per request, counted atomically because the
// handler runs on the server's goroutine and the run on the test's.
//
// The runner, the accumulator and the SSE decoder are the parts of this that
// pair a tool result with its call, and none of them are ours. Driving them
// over a real socket is the only way a test can claim the correlation holds
// where it actually has to, rather than in a hand-built imitation of the
// runner that would agree with whatever the code did.
func stub(t *testing.T, turns ...string) *sdk.Client {
	t.Helper()
	var served atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		turn := int(served.Add(1)) - 1
		if turn >= len(turns) {
			t.Errorf("the runner asked for turn %d, which this test did not script", turn+1)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := w.Write([]byte(turns[turn])); err != nil {
			t.Errorf("serving turn %d: %v", turn+1, err)
		}
	}))
	t.Cleanup(server.Close)
	client := sdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test"))
	return &client
}

// sse frames JSON payloads the way the API delivers them, naming each event
// from its own type so a script cannot disagree with itself.
func sse(t *testing.T, payloads ...string) string {
	t.Helper()
	var body strings.Builder
	for _, payload := range payloads {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &head); err != nil {
			t.Fatalf("framing %s: %v", payload, err)
		}
		body.WriteString("event: " + head.Type + "\ndata: " + payload + "\n\n")
	}
	return body.String()
}

// arguments is a tool call's whole input as the one delta the API would send,
// built through the encoder so the escaping is the wire's and not the test's.
func arguments(t *testing.T, index int, input string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": input},
	})
	if err != nil {
		t.Fatalf("building the delta: %v", err)
	}
	return string(payload)
}

// collect drains a run, failing outside the loop because a range-over-func
// body is not a safe place to end a goroutine.
func collect(t *testing.T, backend *Backend, request nacelle.Request) []nacelle.Event {
	t.Helper()
	var collected []nacelle.Event
	var failure error
	for event, err := range backend.Stream(context.Background(), request) {
		if err != nil {
			failure = err
			break
		}
		collected = append(collected, event)
	}
	if failure != nil {
		t.Fatalf("stream: %v", failure)
	}
	return collected
}

// echoTool answers with whatever it was given, so a result identifies the call
// it came from.
func echoTool(t *testing.T) nacelle.Tool {
	t.Helper()
	type input struct {
		Text string `json:"text" jsonschema:"required,description=What to say back"`
	}
	tool, err := nacelle.NewTool("echo", "Say it back", func(_ context.Context, in input) (string, error) {
		return in.Text, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	return tool
}

// messageStart opens a scripted turn.
func messageStart() string {
	return `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant",` +
		`"model":"claude-opus-5","content":[],"stop_reason":null,"stop_sequence":null,` +
		`"usage":{"input_tokens":10,"output_tokens":1}}}`
}

// messageDelta ends a scripted turn with the reason the model gave.
func messageDelta(reason string) string {
	return `{"type":"message_delta","delta":{"stop_reason":"` + reason +
		`","stop_sequence":null},"usage":{"output_tokens":5}}`
}

// raw builds a stream event the way the wire does, so the test exercises the
// SDK's own decoding of the union rather than a hand-filled struct that could
// disagree with it.
func raw(t *testing.T, payload string) sdk.BetaRawMessageStreamEventUnion {
	t.Helper()
	var event sdk.BetaRawMessageStreamEventUnion
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("decoding %s: %v", payload, err)
	}
	return event
}
