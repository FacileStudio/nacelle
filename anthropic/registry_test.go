package anthropic

import (
	"testing"

	"github.com/FacileStudio/nacelle"
)

// The runner re-encodes a decoded input before handing it to a tool, so the
// bytes a handler sees are not the bytes the model streamed. Matching on them
// raw would miss every call whose arguments the model did not write in sorted,
// compact form.
func TestArgumentsMatchWhateverSpellingTheyArriveIn(t *testing.T) {
	pending := register(&nacelle.ToolEvent{ID: "toolu_1", Index: 3, Name: "echo", Input: `{ "b": 2, "a": 1 }`})

	call, arguments := pending.take("echo", []byte(`{"a":1,"b":2}`))
	if call.ID != "toolu_1" || call.Index != 3 {
		t.Errorf("call = %+v, want toolu_1 at 3; the re-encoded input did not match", call)
	}
	if string(arguments) != `{ "b": 2, "a": 1 }` {
		t.Errorf("arguments = %q, want the bytes the model wrote", arguments)
	}
	if again, _ := pending.take("echo", []byte(`{"a":1,"b":2}`)); again.ID != "" {
		t.Errorf("call = %+v, want nothing; a claimed invocation was handed out twice", again)
	}
}

// Two shapes canonical cannot reproduce, both measured against the real
// runner: a duplicate key, which decodes to the last value here and the first
// there, and invalid JSON, which survives here and reaches the runner as an
// empty object. A miss used to ship the zero Invocation, so the result carried
// no id and sorted as the turn's first call. Within a turn the name is enough
// to recover the right call, which is what the fallback does.
func TestASpellingCanonicalCannotReproduceStillPairs(t *testing.T) {
	shapes := map[string]struct{ streamed, runner string }{
		"a duplicate key": {streamed: `{"a":1,"a":2}`, runner: `{"a":1}`},
		"truncated json":  {streamed: `{"q":`, runner: `{}`},
	}
	for name, shape := range shapes {
		pending := register(&nacelle.ToolEvent{ID: "toolu_1", Index: 2, Name: "echo", Input: shape.streamed})

		call, arguments := pending.take("echo", []byte(shape.runner))
		if call.ID != "toolu_1" || call.Index != 2 {
			t.Errorf("%s: call = %+v, want toolu_1 at 2; the result would ship no id and sort first", name, call)
		}
		if string(arguments) != shape.streamed {
			t.Errorf("%s: arguments = %q, want the bytes the model wrote", name, arguments)
		}
	}
}

// Exact arguments still win where they can, which is what tells two calls to
// one tool apart. Falling back by name unconditionally would pair them by
// whichever execution asked first, and executions run in parallel.
func TestTwoCallsToOneToolKeepTheirOwnArguments(t *testing.T) {
	pending := register(
		&nacelle.ToolEvent{ID: "toolu_1", Index: 0, Name: "echo", Input: `{"text":"first"}`},
		&nacelle.ToolEvent{ID: "toolu_2", Index: 1, Name: "echo", Input: `{"text":"second"}`},
	)

	second, _ := pending.take("echo", []byte(`{"text":"second"}`))
	first, _ := pending.take("echo", []byte(`{"text":"first"}`))
	if first.ID != "toolu_1" || second.ID != "toolu_2" {
		t.Errorf("first = %+v and second = %+v, want each call's own id whichever order they ran in", first, second)
	}
}

// A turn's entries must not survive it. One that does is not a leak, it is a
// live key: the next turn's call with the same name and arguments pops it and
// ships a dead call's id and index.
func TestATurnsCallsDoNotOutliveTheTurn(t *testing.T) {
	pending := newInvocations()
	closeTurn(pending, false, &nacelle.ToolEvent{ID: "toolu_1", Index: 0, Name: "echo", Input: `{"text":"same"}`})
	closeTurn(pending, false, &nacelle.ToolEvent{ID: "toolu_2", Index: 0, Name: "echo", Input: `{"text":"same"}`})

	call, _ := pending.take("echo", []byte(`{"text":"same"}`))
	if call.ID != "toolu_2" {
		t.Errorf("call = %+v, want toolu_2; the previous turn's entry was still claimable", call)
	}
	if again, _ := pending.take("echo", []byte(`{"text":"same"}`)); again.ID != "" {
		t.Errorf("call = %+v, want nothing left from an earlier turn", again)
	}
}

// register files one turn's calls, which is the only way anything reaches the
// registry.
func register(calls ...*nacelle.ToolEvent) *invocations {
	pending := newInvocations()
	pending.reset(calls)
	return pending
}

// closeTurn is a turn ending with these calls, so a test can watch what one
// turn leaves behind for the next.
func closeTurn(pending *invocations, thinking bool, calls ...*nacelle.ToolEvent) {
	newCallTracker(pending, thinking).pending.reset(calls)
}
