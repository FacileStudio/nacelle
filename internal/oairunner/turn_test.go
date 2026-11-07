package oairunner

import (
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

// chunkFromJSON unmarshals the chunk the way the streaming client does, so the
// accumulated tool-call unions carry the raw JSON the client produced.
func chunkFromJSON(t *testing.T, raw string) oai.ChatCompletionChunk {
	t.Helper()
	var chunk oai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		t.Fatalf("unmarshalling chunk: %v", err)
	}
	return chunk
}

// finished seals a turn built from one chunk and returns its message.
func finished(t *testing.T, chunk string) nacelle.Message {
	t.Helper()
	state := turnStream{backend: &Backend{}, stop: nacelle.StopTools}
	state.accumulator.AddChunk(chunkFromJSON(t, chunk))
	events, err := state.finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	last := events[len(events)-1]
	if last.Kind != nacelle.KindMessage {
		t.Fatalf("finish ended on %q, want the turn's message", last.Kind)
	}
	return *last.Message
}

// callsIn lists the tool calls a message carries.
func callsIn(message nacelle.Message) []nacelle.ToolCall {
	var calls []nacelle.ToolCall
	for _, part := range message.Parts {
		if call, ok := part.(nacelle.ToolCall); ok {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestFinishDropsEmptyToolCallEntries(t *testing.T) {
	message := finished(t, `{
		"choices": [{
			"delta": {"tool_calls": [
				{"index": 0, "id": "call_real", "type": "function",
				 "function": {"name": "read_file", "arguments": "{}"}},
				{"index": 1}
			]},
			"finish_reason": "tool_calls"
		}]
	}`)
	calls := callsIn(message)
	if len(calls) != 1 || calls[0].Name != "read_file" {
		t.Fatalf("calls = %+v, want only the real call", calls)
	}

	replayed, err := (&Backend{}).convert(message)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(replayed) != 1 || len(replayed[0].OfAssistant.ToolCalls) != 1 {
		t.Fatalf("replayed = %+v, want one assistant message with one tool call", replayed)
	}
}

func TestFinishKeepsRealToolCalls(t *testing.T) {
	message := finished(t, `{
		"choices": [{
			"delta": {"tool_calls": [
				{"index": 0, "id": "a", "type": "function",
				 "function": {"name": "read_file", "arguments": "{}"}},
				{"index": 1, "id": "b", "type": "function",
				 "function": {"name": "grep", "arguments": "{\"pattern\":\"x\"}"}}
			]},
			"finish_reason": "tool_calls"
		}]
	}`)
	if calls := callsIn(message); len(calls) != 2 {
		t.Fatalf("calls = %+v, want both real calls kept", calls)
	}
}
