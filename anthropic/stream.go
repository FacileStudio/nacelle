package anthropic

import (
	"context"
	"iter"

	"github.com/FacileStudio/nacelle"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

// Stream runs the conversation on the SDK's streaming tool runner.
func (b *Backend) Stream(ctx context.Context, request nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		sink := &nacelle.ToolSink{}
		runner := b.client.Beta.Messages.NewToolRunnerStreaming(adapt(request.Tools, sink), b.params(request))
		out := &emitter{yield: yield, sink: sink}

		if total, ok := runTurns(ctx, runner, out); ok {
			out.send(nacelle.Event{Kind: nacelle.KindDone, Usage: total})
		}
	}
}

// runTurns drives the runner to completion, returning what the run cost and
// whether it finished well enough to report a KindDone.
func runTurns(ctx context.Context, runner *sdk.BetaToolRunnerStreaming, out *emitter) (nacelle.Usage, bool) {
	var total nacelle.Usage

	for turn, err := range runner.AllStreaming(ctx) {
		if err != nil {
			out.fail(err)
			return total, false
		}
		if !streamTurn(turn, out, &total) {
			return total, false
		}
	}

	if err := runner.Err(); err != nil {
		out.fail(err)
		return total, false
	}
	return total, out.flushTools()
}

// streamTurn maps one assistant turn onto the event stream, adding what it
// cost to total. It reports whether the consumer is still ranging.
func streamTurn(turn iter.Seq2[sdk.BetaRawMessageStreamEventUnion, error], out *emitter, total *nacelle.Usage) bool {
	calls := newCallTracker()

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
		if !out.sendAll(turnEnd(event, total)) {
			return false
		}
	}

	return out.flushTools()
}

// turnEnd reports what a turn cost, once the turn is over. Anything that is
// not the end of a turn adds nothing.
func turnEnd(event sdk.BetaRawMessageStreamEventUnion, total *nacelle.Usage) []nacelle.Event {
	if event.Type != "message_delta" {
		return nil
	}
	usage := usageOf(event.Usage)
	*total = total.Add(usage)
	return []nacelle.Event{{Kind: nacelle.KindTurn, Usage: usage}}
}

// emitter is the one place this backend writes to the consumer.
//
// It exists so the loop reads as the sequence of things that happen rather
// than as a yield-guard between every pair of statements, and so the tool sink
// is drained through the same door as everything else.
type emitter struct {
	yield func(nacelle.Event, error) bool
	sink  *nacelle.ToolSink
}

// send yields one event and reports whether the consumer is still ranging.
func (e *emitter) send(event nacelle.Event) bool {
	return e.yield(event, nil)
}

// sendAll yields events in order, stopping at the first refusal.
func (e *emitter) sendAll(events []nacelle.Event) bool {
	for _, event := range events {
		if !e.send(event) {
			return false
		}
	}
	return true
}

// fail ends the sequence with an error.
func (e *emitter) fail(err error) {
	e.yield(nacelle.Event{}, err)
}

// flushTools yields the tool results collected since the last flush.
//
// Handlers run concurrently on the runner's goroutines while the stream is
// pulled from one, so their results are parked in the sink and released here,
// in the goroutine that owns the sequence.
func (e *emitter) flushTools() bool {
	return e.sendAll(e.sink.Drain())
}
