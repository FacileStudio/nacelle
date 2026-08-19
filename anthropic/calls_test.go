package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// A tool call arrives in pieces and must be reported whole: emitting it at
// content_block_start would report a call whose arguments are still empty.
func TestToolCallIsReportedOnceItsInputIsComplete(t *testing.T) {
	tracker := newCallTracker(newInvocations(), false)

	if got := tracker.consume(raw(t, `{"type":"content_block_start","index":0,
		"content_block":{"type":"tool_use","id":"toolu_1","name":"search_events"}}`)); len(got) != 0 {
		t.Fatalf("the start of a tool call emitted %d events, want 0", len(got))
	}
	tracker.consume(argumentFragment(t, 0, `{"query":`))
	tracker.consume(argumentFragment(t, 0, `"deploys"}`))

	events := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	if len(events) != 1 || events[0].Kind != nacelle.KindToolCall {
		t.Fatalf("the end of a tool call emitted %+v, want one KindToolCall", events)
	}
	call := events[0].Tool
	if call.ID != "toolu_1" || call.Name != "search_events" || call.Input != `{"query":"deploys"}` {
		t.Errorf("tool = %+v, want the call reassembled whole", call)
	}
}

// Two tools requested in one turn interleave on the wire, so the index is what
// keeps their arguments apart.
func TestConcurrentToolCallsDoNotMixTheirInput(t *testing.T) {
	tracker := newCallTracker(newInvocations(), false)
	tracker.consume(raw(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"first"}}`))
	tracker.consume(raw(t, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"b","name":"second"}}`))
	tracker.consume(argumentFragment(t, 0, `{"a":1}`))
	tracker.consume(argumentFragment(t, 1, `{"b":2}`))

	first := tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	second := tracker.consume(raw(t, `{"type":"content_block_stop","index":1}`))

	if len(first) != 1 || first[0].Tool.Input != `{"a":1}` {
		t.Errorf("first call = %+v, want its own arguments", first)
	}
	if len(second) != 1 || second[0].Tool.Input != `{"b":2}` {
		t.Errorf("second call = %+v, want its own arguments", second)
	}
}

func TestTextBecomesDeltas(t *testing.T) {
	tracker := newCallTracker(newInvocations(), false)

	text := tracker.consume(raw(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`))
	if len(text) != 1 || text[0].Kind != nacelle.KindText || text[0].Text != "hello" {
		t.Errorf("text delta = %+v, want a KindText event carrying hello", text)
	}
}

// nacelle.Config documents Thinking as streaming reasoning to the consumer and
// leaves it off, so a run that never asked for it must not be handed it — and
// must not differ from the OpenRouter backend, which drops the same deltas.
func TestReasoningIsOnlyStreamedWhenItWasAskedFor(t *testing.T) {
	delta := `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`

	silent := newCallTracker(newInvocations(), false).consume(raw(t, delta))
	if len(silent) != 0 {
		t.Errorf("a run that did not ask for reasoning was sent %+v", silent)
	}

	asked := newCallTracker(newInvocations(), true).consume(raw(t, delta))
	if len(asked) != 1 || asked[0].Kind != nacelle.KindThinking || asked[0].Text != "hmm" {
		t.Errorf("thinking delta = %+v, want a KindThinking event carrying hmm", asked)
	}
}

// The runner skips every tool_use block before the last fallback block: they
// belong to the attempt that refused, and the fallback middleware strips them
// from the replayed history. Registering them anyway leaves entries nothing
// will ever claim, which is how a later call with the same name and arguments
// pops the wrong one — and reporting them without ever closing them leaves a
// consumer waiting on a call that was already abandoned.
func TestAFallbackDiscardsTheCallsBeforeIt(t *testing.T) {
	pending := newInvocations()
	tracker := newCallTracker(pending, false)

	tracker.consume(raw(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"refused","name":"echo"}}`))
	tracker.consume(argumentFragment(t, 0, `{"text":"same"}`))
	tracker.consume(raw(t, `{"type":"content_block_stop","index":0}`))
	discarded := tracker.consume(raw(t, `{"type":"content_block_start","index":1,"content_block":{"type":"fallback"}}`))
	tracker.consume(raw(t, `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"retried","name":"echo"}}`))
	tracker.consume(argumentFragment(t, 2, `{"text":"same"}`))
	tracker.consume(raw(t, `{"type":"content_block_stop","index":2}`))
	tracker.consume(raw(t, `{"type":"message_stop"}`))

	if len(discarded) != 1 || discarded[0].Tool.ID != "refused" || discarded[0].Tool.Err == nil {
		t.Fatalf("the fallback emitted %+v, want the refused call closed with an error", discarded)
	}
	call, _ := pending.take("echo", []byte(`{"text":"same"}`))
	if call.ID != "retried" {
		t.Errorf("call = %+v, want the block after the fallback; the skipped one was still registered", call)
	}
	if again, _ := pending.take("echo", []byte(`{"text":"same"}`)); again.ID != "" {
		t.Errorf("call = %+v, want nothing left; a call the runner never executes was registered", again)
	}
}

// argumentFragment is one slice of a tool call's JSON arguments, built through
// the encoder so a quote in the fragment cannot break the payload around it.
func argumentFragment(t *testing.T, index int, fragment string) sdk.BetaRawMessageStreamEventUnion {
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
