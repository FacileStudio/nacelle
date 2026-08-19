package openrouter

import "github.com/FacileStudio/nacelle"

// emitter is the one place this backend writes to the consumer.
type emitter struct {
	yield func(nacelle.Event, error) bool
	sink  *nacelle.ToolSink
}

// send yields one event and reports whether the consumer is still ranging.
func (e *emitter) send(event nacelle.Event) bool { return e.yield(event, nil) }

// sendAll yields events in order, stopping at the first refusal.
func (e *emitter) sendAll(events []nacelle.Event) bool {
	for _, event := range events {
		if !e.send(event) {
			return false
		}
	}
	return true
}

// fail ends the sequence with an error, classified on the way out.
//
// Classification lives here rather than at the one call site that used to do
// it because this is the only door: OpenRouter reports rate limits and
// upstream failures inside a 200 response, so whether a run is worth retrying
// is decided by parsing the payload, and an error path added later that
// skipped that step would silently turn every retryable failure permanent.
// Doing it at the door means a new path cannot forget.
func (e *emitter) fail(err error) { e.yield(nacelle.Event{}, classify(err)) }

// flushTools yields the tool results collected since the last flush.
func (e *emitter) flushTools() bool { return e.sendAll(e.sink.Drain()) }
