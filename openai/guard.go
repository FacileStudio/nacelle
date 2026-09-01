package openai

import "github.com/FacileStudio/nacelle"

func refuse(turn *turnResult, iteration, max int) (nacelle.Stop, bool) {
	if len(turn.calls) == 0 {
		return turn.stop, true
	}
	if max > 0 && iteration >= max {
		return nacelle.StopIterations, true
	}
	return nacelle.StopOther, false
}

func announce(calls []toolCall, out *emitter) error {
	for index, invocation := range calls {
		if !out.send(invocation.event(index)) {
			return errStopped
		}
	}
	return nil
}
