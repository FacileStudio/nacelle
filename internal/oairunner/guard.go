// Package oairunner runs agents on OpenAI-compatible APIs.
package oairunner

import "github.com/FacileStudio/nacelle"

func refuse(turn *turnResult, iteration, limit int) (nacelle.Stop, bool) {
	switch {
	case len(turn.calls) == 0:
		return settled(turn.stop), true
	case turn.stop != nacelle.StopTools:
		return turn.stop, true
	case limit > 0 && iteration >= limit:
		return nacelle.StopIterations, true
	}
	return "", false
}

func settled(stop nacelle.Stop) nacelle.Stop {
	if stop == nacelle.StopTools {
		return nacelle.StopOther
	}
	return stop
}

func announce(calls []toolCall, out *emitter) error {
	for index, invocation := range calls {
		if !out.send(invocation.event(index)) {
			return errStopped
		}
	}
	return nil
}
