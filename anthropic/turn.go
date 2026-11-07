package anthropic

import (
	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// turnEnd reports what a turn cost and why it ended, once the turn is over.
// Anything that is not the end of a turn adds nothing.
func turnEnd(event sdk.BetaRawMessageStreamEventUnion) []nacelle.Event {
	if event.Type != "message_delta" {
		return nil
	}
	return []nacelle.Event{{
		Kind:  nacelle.KindTurn,
		Usage: usageOf(event.Usage),
		Stop:  stopOf(event.Delta.StopReason),
	}}
}

// usageOf converts the API's per-turn accounting into ours.
func usageOf(usage sdk.BetaMessageDeltaUsage) nacelle.Usage {
	return nacelle.Usage{
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheReadTokens:     usage.CacheReadInputTokens,
		CacheCreationTokens: usage.CacheCreationInputTokens,
	}
}

// stopOf names why a turn ended.
//
// Mapping rather than passing the API's string through is what lets a
// consumer act on the answer being incomplete without learning the provider's
// vocabulary, and it is why the unrecognised case is StopOther rather than a
// new name invented per reason: a stop reason this package has never seen is
// still not an ending anyone should present as a finished answer.
//
// stop_sequence joins end_turn because the caller chose the marker the model
// stopped at, so the text before it is the answer that was asked for.
// compaction and pause_turn land in StopOther deliberately: neither says the
// answer is whole, and neither is something a caller can act on today.
//
// pause_turn is the one that would have to move. It is not Anthropic reporting
// an ending at all, it is Anthropic asking for the turn to be continued, and
// it becomes reachable the moment a server-side tool goes on a request. A run
// that stopped there would carry a half-finished answer under a name that says
// nothing about why. See isToolUse in calls.go, which is the other half of
// that same change and must move with this one.
func stopOf(reason sdk.BetaStopReason) nacelle.Stop {
	switch reason {
	case sdk.BetaStopReasonEndTurn, sdk.BetaStopReasonStopSequence:
		return nacelle.StopEnd
	case sdk.BetaStopReasonToolUse:
		return nacelle.StopTools
	case sdk.BetaStopReasonMaxTokens:
		return nacelle.StopMaxTokens
	case sdk.BetaStopReasonModelContextWindowExceeded:
		return nacelle.StopContext
	case sdk.BetaStopReasonRefusal:
		return nacelle.StopRefusal
	default:
		return nacelle.StopOther
	}
}
