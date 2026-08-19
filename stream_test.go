package nacelle

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// raw builds a stream event the way the wire does, so the test exercises the
// SDK's own decoding of the union rather than a hand-filled struct that could
// disagree with it.
func raw(t *testing.T, payload string) anthropic.BetaRawMessageStreamEventUnion {
	t.Helper()
	var event anthropic.BetaRawMessageStreamEventUnion
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("decoding %s: %v", payload, err)
	}
	return event
}

// argumentFragment is one slice of a tool call's JSON arguments, built through
// the encoder so a quote in the fragment cannot break the payload around it.
func argumentFragment(t *testing.T, index int, fragment string) anthropic.BetaRawMessageStreamEventUnion {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": fragment},
	})
	if err != nil {
		t.Fatalf("building the delta: %v", err)
	}
	return raw(t, string(payload))
}

// A tool call arrives in pieces and must be reported whole: emitting it at
// content_block_start would report a call whose arguments are still empty.
func TestToolCallIsReportedOnceItsInputIsComplete(t *testing.T) {
	tracker := newCallTracker()

	if got := tracker.consume(raw(t, `{"type":"content_block_start","index":0,
		"content_block":{"type":"tool_use","id":"toolu_1","name":"search_events"}}`)); len(got) != 0 {
		t.Fatalf("the start of a tool call emitted %d events, want 0", len(got))
	}

	if got := tracker.consume(argumentFragment(t, 0, `{"query":`)); len(got) != 0 {
		t.Fatalf("an argument fragment emitted %d events, want 0", len(got))
	}
	tracker.consume(argumentFragment(t, 0, `"deploys"}`))

	events := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	if len(events) != 1 {
		t.Fatalf("the end of a tool call emitted %d events, want 1", len(events))
	}
	call := events[0]
	if call.Kind != KindToolCall {
		t.Errorf("kind = %q, want %q", call.Kind, KindToolCall)
	}
	if call.Tool.ID != "toolu_1" || call.Tool.Name != "search_events" {
		t.Errorf("tool = %+v, want id toolu_1 and name search_events", call.Tool)
	}
	if call.Tool.Input != `{"query":"deploys"}` {
		t.Errorf("input = %q, want the reassembled arguments", call.Tool.Input)
	}
}

// A tool this process runs and one the API runs over MCP are the same event to
// a consumer, and only the block type distinguishes them.
func TestMCPToolCallsAreTrackedToo(t *testing.T) {
	tracker := newCallTracker()
	tracker.consume(raw(t, `{"type":"content_block_start","index":0,
		"content_block":{"type":"mcp_tool_use","id":"mcp_1","name":"get_entity"}}`))

	events := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	if len(events) != 1 || events[0].Tool.Name != "get_entity" {
		t.Fatalf("an MCP tool call was not tracked: %+v", events)
	}
}

// Two tools requested in one turn interleave on the wire, so the index is what
// keeps their arguments apart.
func TestConcurrentToolCallsDoNotMixTheirInput(t *testing.T) {
	tracker := newCallTracker()
	tracker.consume(raw(t, `{"type":"content_block_start","index":0,
		"content_block":{"type":"tool_use","id":"a","name":"first"}}`))
	tracker.consume(raw(t, `{"type":"content_block_start","index":1,
		"content_block":{"type":"tool_use","id":"b","name":"second"}}`))
	tracker.consume(raw(t, `{"type":"content_block_delta","index":0,
		"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`))
	tracker.consume(raw(t, `{"type":"content_block_delta","index":1,
		"delta":{"type":"input_json_delta","partial_json":"{\"b\":2}"}}`))

	first := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	second := tracker.consume(raw(t, `{"type":"content_block_stop","index":1}`))

	if len(first) != 1 || first[0].Tool.Input != `{"a":1}` {
		t.Errorf("first call = %+v, want its own arguments", first)
	}
	if len(second) != 1 || second[0].Tool.Input != `{"b":2}` {
		t.Errorf("second call = %+v, want its own arguments", second)
	}
}

func TestTextAndThinkingBecomeDeltas(t *testing.T) {
	tracker := newCallTracker()

	text := tracker.consume(raw(t, `{"type":"content_block_delta","index":0,
		"delta":{"type":"text_delta","text":"hello"}}`))
	if len(text) != 1 || text[0].Kind != KindText || text[0].Text != "hello" {
		t.Errorf("text delta = %+v, want a KindText event carrying hello", text)
	}

	thinking := tracker.consume(raw(t, `{"type":"content_block_delta","index":0,
		"delta":{"type":"thinking_delta","thinking":"hmm"}}`))
	if len(thinking) != 1 || thinking[0].Kind != KindThinking || thinking[0].Text != "hmm" {
		t.Errorf("thinking delta = %+v, want a KindThinking event carrying hmm", thinking)
	}
}

// Tool results are produced by handlers the runner calls concurrently, while
// the stream is pulled from one goroutine, so they are parked and drained.
func TestToolSinkDrainsOnce(t *testing.T) {
	sink := &toolSink{}
	sink.push(Event{Kind: KindToolResult})
	sink.push(Event{Kind: KindToolResult})

	if drained := sink.drain(); len(drained) != 2 {
		t.Fatalf("drained %d events, want 2", len(drained))
	}
	if drained := sink.drain(); len(drained) != 0 {
		t.Errorf("a second drain returned %d events, want 0", len(drained))
	}
}
