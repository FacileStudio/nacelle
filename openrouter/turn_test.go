package openrouter

import (
	"testing"

	"github.com/FacileStudio/nacelle"
)

// Not every provider behind the gateway sends the usage chunk. The stream is
// otherwise ordinary: an answer, a finish reason, and no accounting at all.
const withoutUsage = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

// The other way to get it wrong: a running total repeated on every chunk,
// where the last figure is the turn's cost and the earlier ones are prefixes
// of it, not extra spending.
const cumulativeUsage = `data: {"id":"g","choices":[{"index":0,"delta":{"role":"assistant","content":"a"},"finish_reason":null}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"cost":0.1}}

data: {"id":"g","choices":[{"index":0,"delta":{"content":"b"},"finish_reason":null}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cost":0.2}}

data: {"id":"g","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13,"cost":0.3}}

data: [DONE]

`

// A turn used to be ended by the usage chunk, so a provider that never sent
// one produced no KindTurn at all: the promise that usage is reported per turn
// broke, and the turn's stop reason went with it.
func TestATurnWithNoUsageChunkStillEndsOnATurnEvent(t *testing.T) {
	backend, _ := serve(t, withoutUsage)
	events := collect(t, backend, nacelle.Request{System: "s"})

	turns := kinds(events, nacelle.KindTurn)
	if len(turns) != 1 {
		t.Fatalf("saw %d turn events, want exactly 1 even with no accounting to report", len(turns))
	}
	if turns[0].Stop != nacelle.StopEnd {
		t.Errorf("turn stop = %q, want the reason the turn ended", turns[0].Stop)
	}
	if turns[0].Usage.Total() != 0 {
		t.Errorf("usage = %+v, want zero rather than invented", turns[0].Usage)
	}
}

// Usage repeated on every chunk is one turn's bill quoted three times. Adding
// it up charges the caller for tokens nobody spent and emits a KindTurn per
// chunk, which turns one turn into three for anything counting them.
func TestRepeatedCumulativeUsageIsOneTurnBilledOnce(t *testing.T) {
	backend, _ := serve(t, cumulativeUsage)
	events := collect(t, backend, nacelle.Request{System: "s"})

	turns := kinds(events, nacelle.KindTurn)
	if len(turns) != 1 {
		t.Fatalf("saw %d turn events, want 1 turn however often the total was repeated", len(turns))
	}
	if turns[0].Usage.InputTokens != 10 || turns[0].Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want the last running total, not the sum of every quote", turns[0].Usage)
	}

	done := kinds(events, nacelle.KindDone)
	if len(done) != 1 || done[0].Usage.Cost != 0.3 {
		t.Errorf("done = %+v, want the run total to match the turn it contains", done)
	}
}
