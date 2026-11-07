package anthropic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// recorder serves scripted turns and keeps every request body it was sent.
type recorder struct {
	t      *testing.T
	mu     sync.Mutex
	turns  []string
	bodies []map[string]any
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		r.t.Errorf("reading the request: %v", err)
	}
	r.mu.Lock()
	turn := len(r.bodies)
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	if turn >= len(r.turns) {
		r.t.Errorf("asked for turn %d, which this test did not script", turn+1)
		return
	}
	if _, err := w.Write([]byte(r.turns[turn])); err != nil {
		r.t.Errorf("serving turn %d: %v", turn+1, err)
	}
}

func recording(t *testing.T, turns ...string) (*sdk.Client, *recorder) {
	t.Helper()
	rec := &recorder{t: t, turns: turns}
	server := httptest.NewServer(rec)
	t.Cleanup(server.Close)
	client := sdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("test"))
	return &client, rec
}

// thinkingThenCall is a turn that reasons, signs the reasoning, and asks for
// echo.
func thinkingThenCall(t *testing.T) string {
	t.Helper()
	return sse(t, messageStart(),
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me check"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
		arguments(t, 1, `{"text":"hi"}`),
		`{"type":"content_block_stop","index":1}`,
		messageDelta("tool_use"), `{"type":"message_stop"}`)
}

// The signed thinking block a tool turn opened with must go back on the next
// request exactly as it came: the API rejects a tool loop whose thinking
// block lost its signature.
func TestASignedThinkingBlockIsSentBackOnTheNextTurn(t *testing.T) {
	client, rec := recording(t, thinkingThenCall(t), answeringTurn(t))
	collect(t, New(Config{Client: client}), nacelle.Request{
		Tools: []nacelle.Tool{echoTool(t)}, MaxTokens: 4096, MaxIterations: 4,
		Thinking: nacelle.Thinking{Show: true, Budget: 1024},
	})

	if len(rec.bodies) != 2 {
		t.Fatalf("made %d requests, want 2", len(rec.bodies))
	}
	messages, _ := rec.bodies[1]["messages"].([]any)
	assistant, _ := messages[0].(map[string]any)
	content, _ := assistant["content"].([]any)
	first, _ := content[0].(map[string]any)
	if first["type"] != "thinking" || first["signature"] != "sig-1" || first["thinking"] != "let me check" {
		t.Fatalf("the replayed turn opened with %v, want the signed thinking block", first)
	}
}

// Every message the model saw arrives as a KindMessage, with the reasoning
// that was shown and the Finish of each turn.
func TestTheRunCommitsItsTranscript(t *testing.T) {
	client, _ := recording(t, thinkingThenCall(t), answeringTurn(t))
	events := collect(t, New(Config{Client: client}), nacelle.Request{
		Tools: []nacelle.Tool{echoTool(t)}, MaxTokens: 4096, MaxIterations: 4,
		Thinking: nacelle.Thinking{Show: true, Budget: 1024},
	})

	var committed []nacelle.Message
	for _, event := range events {
		if event.Kind == nacelle.KindMessage {
			committed = append(committed, *event.Message)
		}
	}
	if len(committed) != 3 {
		t.Fatalf("committed %d messages, want the call, its result and the answer", len(committed))
	}
	parts := committed[0].Parts
	if reasoning, ok := parts[0].(nacelle.Reasoning); !ok || reasoning.Text != "let me check" {
		t.Errorf("the first turn opened with %#v, want the shown reasoning", parts[0])
	}
	if finish, ok := parts[len(parts)-1].(nacelle.Finish); !ok || finish.Stop != nacelle.StopTools {
		t.Errorf("the first turn ended with %#v, want its Finish", parts[len(parts)-1])
	}
	if result, ok := committed[1].Parts[0].(nacelle.ToolResult); !ok || result.ID != "toolu_1" || result.Result != "hi" {
		t.Errorf("the result turn is %#v, want echo's answer to toolu_1", committed[1].Parts)
	}
}
