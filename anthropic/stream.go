package anthropic

import (
	"context"
	"iter"

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

// Stream runs the conversation on the SDK's streaming tool runner.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		sink := &nacelle.ToolSink{}
		pending := newInvocations()
		runner := b.client.Beta.Messages.NewToolRunnerStreaming(adapt(request.Tools, sink, pending), b.params(request))
		out := &emitter{yield: yield, sink: sink}

		if run, ok := runTurns(ctx, runner, out, pending); ok {
			out.send(nacelle.Event{Kind: nacelle.KindDone, Usage: run.usage, Stop: run.stop})
		}
	}
}

// runTurns drives the runner to completion, returning what the run cost, why
// it ended, and whether it finished well enough to report a KindDone.
func runTurns(ctx context.Context, runner *sdk.BetaToolRunnerStreaming, out *emitter, pending *invocations) (outcome, bool) {
	var run outcome

	for turn, err := range runner.AllStreaming(ctx) {
		if err != nil {
			out.fail(err)
			return run, false
		}
		if !streamTurn(turn, out, &run, pending) {
			return run, false
		}
	}

	if err := runner.Err(); err != nil {
		out.fail(err)
		return run, false
	}
	run.stop = finalStop(runner, run.stop)
	return run, out.flushTools()
}

// streamTurn maps one assistant turn onto the event stream, adding what it
// cost to the run. It reports whether the consumer is still ranging.
func streamTurn(turn iter.Seq2[sdk.BetaRawMessageStreamEventUnion, error], out *emitter, run *outcome, pending *invocations) bool {
	calls := newCallTracker(pending)

	for event, err := range turn {
		if err != nil {
			out.fail(err)
			return false
		}
		if !out.flushTools() {
			return false
		}
		if !out.sendAll(calls.consume(event)) {
			return false
		}
		if !out.sendAll(turnEnd(event, run)) {
			return false
		}
	}

	return out.flushTools()
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
