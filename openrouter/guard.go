package openrouter

import "github.com/FacileStudio/nacelle"

// refuse decides whether this turn's tool calls may run, and what the run ends
// as when they may not. It reports the reason and whether the loop is over.
//
// Deciding before execution rather than after is the whole difference. The
// Anthropic SDK checks the cap before it runs anything, so a caller who sets
// MaxIterations as a blast radius around a tool that writes files gets exactly
// what they asked for; a backend that checks at the top of the next iteration
// has already run them by then, and the fence is decoration. The same argument
// covers truncation: a turn cut off by the output ceiling left its last call's
// arguments half-written, and arguments that happen to still parse are the
// dangerous case, not the safe one — the second call is cut while the first is
// intact, so the model's real request runs with an invented tail.
//
// A turn asking for no tools ends the run whatever it said, which is where the
// finish reasons that do not fit go: see settled.
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

// settled is the reason a run ended, for a turn that asked for nothing more.
//
// It exists for one value. StopTools promises more turns and is documented as
// never being why a run ended, yet OpenRouter reaches it with an empty hand
// two ways: finish_reason "function_call" is the deprecated spelling of a tool
// request and carries no tool_calls array at all, and a provider is free to
// send "tool_calls" with an array this package cannot parse. Reporting either
// as StopTools tells a consumer to wait for turns that will never come, so
// they become StopOther: unnameable, and never mistaken for a finished answer.
func settled(stop nacelle.Stop) nacelle.Stop {
	if stop == nacelle.StopTools {
		return nacelle.StopOther
	}
	return stop
}

// announce tells the consumer what the model asked for without running any of
// it, and reports errStopped when the consumer has abandoned the sequence.
//
// The events go out because the alternative is a silent ending: a run capped
// at its last iteration is unfinished work, and the calls it stopped short of
// are the most useful thing it can say about what was left. Anthropic behaves
// the same way for the same reason — the tool_use blocks stream out as part of
// the turn and the runner then declines to execute them. The absence of a
// KindToolResult is the signal that nothing ran, and it is the only one, so a
// consumer pairing calls to results by ID must tolerate a call with no answer.
func announce(calls []toolCall, out *emitter) error {
	for index, invocation := range calls {
		if !out.send(invocation.event(index)) {
			return errStopped
		}
	}
	return nil
}
