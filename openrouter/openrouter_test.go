package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// stream serves canned SSE bodies, one per request, and records what it was
// asked. It is deliberately a real HTTP server: the parsing is where this
// backend's bugs would live, and a fake client would skip exactly that.
type stream struct {
	bodies   []string
	requests []map[string]any
}

func (s *stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.requests = append(s.requests, decoded)

	body := s.bodies[min(len(s.requests)-1, len(s.bodies)-1)]
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, body); err != nil {
		panic(err)
	}
}

func serve(t *testing.T, bodies ...string) (*Backend, *stream) {
	t.Helper()
	handler := &stream{bodies: bodies}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	backend, err := New(Config{Model: "test/model", APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return backend, handler
}

// collect drains a run into a slice, failing on the first error.
func collect(t *testing.T, backend *Backend, request nacelle.Request) []nacelle.Event {
	t.Helper()
	var events []nacelle.Event
	for event, err := range backend.Stream(context.Background(), request) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func kinds(events []nacelle.Event, kind nacelle.Kind) []nacelle.Event {
	var found []nacelle.Event
	for _, event := range events {
		if event.Kind == kind {
			found = append(found, event)
		}
	}
	return found
}

// The keepalive comment is the documented crash: passing `: OPENROUTER
// PROCESSING` to a JSON decoder throws, and unhandled it takes the loop with
// it. The blank line that follows it is the second half of the same trap.
const withKeepalive = `: OPENROUTER PROCESSING

data: {"id":"gen-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"},"finish_reason":null}]}

: OPENROUTER PROCESSING

data: {"id":"gen-1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}

data: {"id":"gen-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"gen-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cost":0.00014}}

data: [DONE]

`

func TestKeepaliveCommentsDoNotBreakTheStream(t *testing.T) {
	backend, _ := serve(t, withKeepalive)
	events := collect(t, backend, nacelle.Request{System: "s", Messages: []nacelle.Message{nacelle.UserText("hi")}})

	var text strings.Builder
	for _, event := range kinds(events, nacelle.KindText) {
		text.WriteString(event.Text)
	}
	if text.String() != "hello" {
		t.Errorf("text = %q, want hello", text.String())
	}
}

// OpenRouter prices the generation and returns the figure. It arrives in the
// final chunk, whose choices array is empty — indexing it unguarded panics on
// every single run.
func TestCostAndUsageComeFromTheFinalChunk(t *testing.T) {
	backend, _ := serve(t, withKeepalive)
	events := collect(t, backend, nacelle.Request{System: "s"})

	turns := kinds(events, nacelle.KindTurn)
	if len(turns) != 1 {
		t.Fatalf("saw %d turn events, want 1", len(turns))
	}
	usage := turns[0].Usage
	if usage.InputTokens != 10 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v, want 10 in and 2 out", usage)
	}
	if usage.Cost != 0.00014 {
		t.Errorf("cost = %v, want 0.00014", usage.Cost)
	}

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Usage.Cost != 0.00014 {
		t.Errorf("done = %+v, want the run total to carry the cost", done)
	}
}

// stream_options.include_usage and usage.include are both deprecated and
// inert. Sending them is harmless but it is cargo cult, and every sample from
// before the change still does it.
func TestTheDeprecatedUsageParametersAreNotSent(t *testing.T) {
	backend, handler := serve(t, withKeepalive)
	collect(t, backend, nacelle.Request{System: "s"})

	request := handler.requests[0]
	if _, sent := request["stream_options"]; sent {
		t.Error("stream_options was sent; include_usage is deprecated and has no effect")
	}
	if _, sent := request["usage"]; sent {
		t.Error("a usage parameter was sent; usage is always included now")
	}
}

// Arguments arrive as fragments keyed by index, and two parallel calls
// interleave in one stream separated only by that index.
