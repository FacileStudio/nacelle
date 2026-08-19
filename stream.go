package nacelle

import (
	"context"
	"iter"

	"github.com/anthropics/anthropic-sdk-go"
)

// Message is one turn of the conversation so far.
type Message struct {
	// Assistant marks a message the model produced rather than the user.
	Assistant bool
	Text      string
}

// Stream runs the conversation and yields what happens as it happens.
//
// The sequence ends after a KindDone event, or early with a non-nil error. A
// consumer that stops ranging cancels the run: the underlying request is torn
// down with the context, so abandoning the loop is a supported way to stop an
// agent rather than a leak.
//
// Tool failures are not stream errors. A tool that returns an error is
// reported as a KindToolResult carrying it and handed back to the model, which
// is better placed than the caller to decide whether the task can still be
// finished. An error out of this sequence means the run itself failed.
func (a *Agent) Stream(ctx context.Context, conversation []Message) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		sink := &toolSink{}
		params := a.params
		params.Messages = toParams(conversation)

		runner := a.client.Beta.Messages.NewToolRunnerStreaming(observe(a.tools, sink), params)
		out := &emitter{yield: yield, sink: sink}

		if total, ok := a.runTurns(ctx, runner, out); ok {
			out.send(Event{Kind: KindDone, Usage: total})
		}
	}
}

// runTurns drives the runner to completion, returning what the run cost and
// whether it finished well enough to report a KindDone.
func (a *Agent) runTurns(ctx context.Context, runner *anthropic.BetaToolRunnerStreaming, out *emitter) (Usage, bool) {
	var total Usage

	for turn, err := range runner.AllStreaming(ctx) {
		if err != nil {
			out.fail(err)
			return total, false
		}
		if !a.streamTurn(turn, out, &total) {
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
func (a *Agent) streamTurn(turn iter.Seq2[anthropic.BetaRawMessageStreamEventUnion, error], out *emitter, total *Usage) bool {
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
func turnEnd(event anthropic.BetaRawMessageStreamEventUnion, total *Usage) []Event {
	if event.Type != "message_delta" {
		return nil
	}
	usage := usageOf(event.Usage)
	*total = total.Add(usage)
	return []Event{{Kind: KindTurn, Usage: usage}}
}

// emitter is the one place this package writes to the consumer.
//
// It exists so the loop reads as the sequence of things that happen rather
// than as a yield-guard between every pair of statements, and so the tool sink
// is drained through the same door as everything else.
type emitter struct {
	yield func(Event, error) bool
	sink  *toolSink
}

// send yields one event and reports whether the consumer is still ranging.
func (e *emitter) send(event Event) bool {
	return e.yield(event, nil)
}

// sendAll yields events in order, stopping at the first refusal.
func (e *emitter) sendAll(events []Event) bool {
	for _, event := range events {
		if !e.send(event) {
			return false
		}
	}
	return true
}

// fail ends the sequence with an error.
func (e *emitter) fail(err error) {
	e.yield(Event{}, err)
}

// flushTools yields the tool results collected since the last flush.
//
// Handlers run concurrently on the runner's goroutines while the stream is
// pulled from one, so their results are parked in the sink and released here,
// in the goroutine that owns the sequence.
func (e *emitter) flushTools() bool {
	return e.sendAll(e.sink.drain())
}
