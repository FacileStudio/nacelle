package anthropic

import (
	"github.com/FacileStudio/nacelle"
)

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

// fail ends the sequence with an error, classified so a transient one can be
// retried. Every error out of this backend passes through here, which is why
// the classification lives at this door rather than at each of its callers.
func (e *emitter) fail(err error) {
	e.yield(nacelle.Event{}, classify(err))
}

// flushTools yields the tool results collected since the last flush.
//
// Handlers run concurrently on the runner's goroutines while the stream is
// pulled from one, so their results are parked in the sink and released here,
// in the goroutine that owns the sequence.
func (e *emitter) flushTools() bool {
	return e.sendAll(e.sink.Drain())
}
