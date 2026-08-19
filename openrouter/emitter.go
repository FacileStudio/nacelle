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

// fail ends the sequence with an error.
func (e *emitter) fail(err error) { e.yield(nacelle.Event{}, err) }

// flushTools yields the tool results collected since the last flush.
func (e *emitter) flushTools() bool { return e.sendAll(e.sink.Drain()) }
