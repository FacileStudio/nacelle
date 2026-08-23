package anthropic

import (
	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// outcome is what a run accumulated: what it cost, and why it ended.
//
// The two travel together because they are filled from the same event and
// reported on the same one. message_delta carries the turn's usage and its
// stop reason, and KindDone carries the run's total of the first and the last
// value of the second.
type outcome struct {
	usage nacelle.Usage
	stop  nacelle.Stop
}

// turnEnd reports what a turn cost and why it ended, once the turn is over.
// Anything that is not the end of a turn adds nothing.
func turnEnd(event sdk.BetaRawMessageStreamEventUnion, run *outcome) []nacelle.Event {
	if event.Type != "message_delta" {
		return nil
	}
	usage := usageOf(event.Usage)
	run.usage = run.usage.Add(usage)
	run.stop = stopOf(event.Delta.StopReason)
	return []nacelle.Event{{Kind: nacelle.KindTurn, Usage: usage, Stop: run.stop}}
}

// finalStop is why the run ended, which is not always why its last turn did.
//
// They differ in one case and it is the one worth catching. The runner stops
// calling the API once MaxIterations is reached, and it stops between turns,
// so the last turn it did make ended with the model still asking for tools
// that will never run. Left alone that reports StopTools, which promises more
// turns and is the documented one reason a run never ends with. A run that
// merely used its last permitted iteration to finish is not capped, which is
// why the pending tools are half of the test rather than the count alone.
//
// Hitting the cap is unfinished work and not a failure, so it is a stop
// reason rather than an error out of the stream.
func finalStop(runner *sdk.BetaToolRunnerStreaming, stop nacelle.Stop) nacelle.Stop {
	limit := runner.Params.MaxIterations
	if stop == nacelle.StopTools && limit > 0 && runner.IterationCount() >= limit {
		return nacelle.StopIterations
	}
	return stop
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
