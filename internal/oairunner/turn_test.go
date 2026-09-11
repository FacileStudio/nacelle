package oairunner

import (
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"

	oai "github.com/openai/openai-go/v3"
)

// chunkFromJSON unmarshals the chunk the way the streaming client does, so the
// accumulated tool-call unions carry the raw JSON that ToParam replays.
func chunkFromJSON(t *testing.T, raw string) oai.ChatCompletionChunk {
	t.Helper()
	var chunk oai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		t.Fatalf("unmarshalling chunk: %v", err)
	}
	return chunk
}

func TestFinishDropsEmptyToolCallEntries(t *testing.T) {
	var state turnStream
	state.backend = &Backend{}
	state.stop = nacelle.StopTools
	state.total = &nacelle.Usage{}
	state.accumulator.AddChunk(chunkFromJSON(t, `{
		"choices": [{
			"delta": {"tool_calls": [
				{"index": 0, "id": "call_real", "type": "function",
				 "function": {"name": "read_file", "arguments": "{}"}},
				{"index": 1}
			]},
			"finish_reason": "tool_calls"
		}]
	}`))

	out := &emitter{yield: func(nacelle.Event, error) bool { return true }}
	result, err := state.finish(out)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if len(result.calls) != 1 || result.calls[0].name != "read_file" {
		t.Fatalf("calls = %+v, want only the real call", result.calls)
	}

	param, err := json.Marshal(result.assistant)
	if err != nil {
		t.Fatalf("marshalling the replayed assistant: %v", err)
	}
	if len(result.assistant.OfAssistant.ToolCalls) != 1 {
		t.Fatalf("replayed assistant = %s, want exactly one tool call", param)
	}
}

func TestFinishKeepsRealToolCalls(t *testing.T) {
	var state turnStream
	state.backend = &Backend{}
	state.stop = nacelle.StopTools
	state.total = &nacelle.Usage{}
	state.accumulator.AddChunk(chunkFromJSON(t, `{
		"choices": [{
			"delta": {"tool_calls": [
				{"index": 0, "id": "a", "type": "function",
				 "function": {"name": "read_file", "arguments": "{}"}},
				{"index": 1, "id": "b", "type": "function",
				 "function": {"name": "search_content", "arguments": "{\"pattern\":\"x\"}"}}
			]},
			"finish_reason": "tool_calls"
		}]
	}`))

	out := &emitter{yield: func(nacelle.Event, error) bool { return true }}
	result, err := state.finish(out)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if len(result.calls) != 2 {
		t.Fatalf("calls = %+v, want both real calls kept", result.calls)
	}
}
