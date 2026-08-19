package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// A run cut off by the output ceiling comes back as a perfectly well-formed
// response with a normal ending, so a consumer that is not told cannot tell a
// truncated answer from a finished one and will present half a sentence as
// the result.
func TestATruncatedTurnDoesNotClaimToBeComplete(t *testing.T) {
	var run outcome
	events := turnEnd(raw(t, `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":9}}`), &run)

	if len(events) != 1 || events[0].Kind != nacelle.KindTurn {
		t.Fatalf("the end of a turn emitted %+v, want one KindTurn", events)
	}
	if events[0].Stop != nacelle.StopMaxTokens {
		t.Errorf("stop = %q, want %q", events[0].Stop, nacelle.StopMaxTokens)
	}
	if events[0].Stop.Complete() {
		t.Error("a turn the output ceiling cut off reports itself as a finished answer")
	}
	if run.stop != nacelle.StopMaxTokens {
		t.Errorf("the run kept %q, want the turn's reason so KindDone can carry it", run.stop)
	}
}

// Every reason the API can give has to land somewhere, and the ones this
// package has no name for must still not read as a finished answer.
func TestEveryStopReasonMapsAndOnlyFinishedOnesLookFinished(t *testing.T) {
	wanted := map[sdk.BetaStopReason]nacelle.Stop{
		sdk.BetaStopReasonEndTurn:                    nacelle.StopEnd,
		sdk.BetaStopReasonStopSequence:               nacelle.StopEnd,
		sdk.BetaStopReasonToolUse:                    nacelle.StopTools,
		sdk.BetaStopReasonMaxTokens:                  nacelle.StopMaxTokens,
		sdk.BetaStopReasonModelContextWindowExceeded: nacelle.StopContext,
		sdk.BetaStopReasonRefusal:                    nacelle.StopRefusal,
		sdk.BetaStopReasonPauseTurn:                  nacelle.StopOther,
		sdk.BetaStopReasonCompaction:                 nacelle.StopOther,
		"a_reason_invented_after_this_was_written":   nacelle.StopOther,
	}
	for reason, want := range wanted {
		if got := stopOf(reason); got != want {
			t.Errorf("stop reason %q mapped to %q, want %q", reason, got, want)
		}
	}
}

// The runner stops calling the API once MaxIterations is reached and reports
// nothing about it, so a capped run ends on a KindDone that looks exactly like
// a finished one while the model was still asking for tools.
func TestRunningOutOfIterationsIsAnEndingWithAName(t *testing.T) {
	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo","input":{}}}`,
			arguments(t, 0, `{"text":"first"}`),
			`{"type":"content_block_stop","index":0}`,
			messageDelta("tool_use"), `{"type":"message_stop"}`),
	)})

	events := collect(t, backend, nacelle.Request{
		Tools:         []nacelle.Tool{echoTool(t)},
		MaxTokens:     1024,
		MaxIterations: 1,
	})

	done := events[len(events)-1]
	if done.Kind != nacelle.KindDone {
		t.Fatalf("the run ended on %+v, want a KindDone", done)
	}
	if done.Stop != nacelle.StopIterations {
		t.Errorf("stop = %q, want %q; a capped run is indistinguishable from a finished one", done.Stop, nacelle.StopIterations)
	}
	if done.Stop.Complete() {
		t.Error("a run that ran out of iterations reports itself as a finished answer")
	}
}

// A run that used its last permitted iteration to answer is not capped, and
// saying it was would report unfinished work that is finished.
func TestFinishingOnTheLastIterationIsNotTheCap(t *testing.T) {
	backend := New(Config{Client: stub(t,
		sse(t, messageStart(),
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"content_block_stop","index":0}`,
			messageDelta("end_turn"), `{"type":"message_stop"}`),
	)})

	events := collect(t, backend, nacelle.Request{MaxTokens: 1024, MaxIterations: 1})

	done := events[len(events)-1]
	if done.Kind != nacelle.KindDone || !done.Stop.Complete() {
		t.Errorf("the run ended on %+v, want a completed KindDone", done)
	}
}
