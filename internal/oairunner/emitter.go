package oairunner

import "github.com/FacileStudio/nacelle"

type emitter struct {
	yield func(nacelle.Event, error) bool
	sink  *nacelle.ToolSink
}

func (e *emitter) send(event nacelle.Event) bool {
	return e.yield(event, nil)
}

func (e *emitter) sendAll(events []nacelle.Event) bool {
	for _, event := range events {
		if !e.send(event) {
			return false
		}
	}
	return true
}

func (e *emitter) fail(err error) {
	e.yield(nacelle.Event{}, err)
}

func (e *emitter) flushTools() bool {
	return e.sendAll(e.sink.Drain())
}