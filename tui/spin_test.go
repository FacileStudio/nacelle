package main

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/FacileStudio/nacelle"
)

// A request can sit a full second or more before its first token, and a
// screen that has not moved since the question was echoed reads exactly like
// a client that stopped responding. The spinner is the only thing on screen
// that proves otherwise.
func TestAskingShowsTheSpinnerBeforeAnythingArrives(t *testing.T) {
	m := sized()
	m.agent = answering(t)
	m.prompt.SetValue("are you there?")
	m.ask()
	defer m.run.cancel()

	if !m.run.waiting {
		t.Fatal("waiting was not set the moment the question was sent")
	}
	if !strings.Contains(visible(m.viewport.View()), "waiting for a response") {
		t.Errorf("viewport = %q, want the spinner line while nothing has arrived yet", m.viewport.View())
	}
}

// Whatever comes back first — text, a tool call, an error — is the model no
// longer being silent. The spinner's job ends there, not at the first token
// specifically.
func TestTheFirstEventEndsTheWaitWhicheverKindItIs(t *testing.T) {
	m := sized()
	m.run.waiting = true

	m.consume(result{event: nacelle.Event{Kind: nacelle.KindText, Text: "h"}})

	if m.run.waiting {
		t.Error("waiting was still true after the first event arrived")
	}
	if strings.Contains(visible(m.viewport.View()), "waiting for a response") {
		t.Errorf("viewport = %q, want the spinner line gone once real content starts", m.viewport.View())
	}
}

// An error is still an answer to "is anything happening" — the spinner never
// promised good news, only that the wait was over.
func TestAnErrorAlsoEndsTheWait(t *testing.T) {
	m := sized()
	m.run.waiting = true

	m.consume(result{err: errors.New("boom")})

	if m.run.waiting {
		t.Error("waiting was still true after the run's first result was an error")
	}
}

// A run whose stream closes without yielding anything at all — an immediate
// cancel, or a backend that errors before its first yield — never reaches
// consume, so the spinner has to be stopped from settle too, or it spins for
// a question already abandoned.
func TestSettleClearsWaitingEvenIfNothingEverArrived(t *testing.T) {
	m := sized()
	m.run.cancel = func() {}
	m.run.waiting = true

	m.settle()

	if m.run.waiting {
		t.Error("waiting survived a run that produced nothing before settling")
	}
}

// The library's Update always hands back a Cmd that re-arms the next frame.
// Not returning it is the only way the loop stops, so this is the one place
// that behaviour is worth locking down directly.
func TestTheSpinnerKeepsTickingWhileWaiting(t *testing.T) {
	m := sized()
	m.run.waiting = true

	msg, ok := m.spin.Tick().(spinner.TickMsg)
	if !ok {
		t.Fatal("Tick did not produce a spinner.TickMsg")
	}
	if cmd := m.spun(msg); cmd == nil {
		t.Fatal("the spinner did not re-arm its next tick while still waiting")
	}
}

func TestTheSpinnerStopsTickingOnceTheWaitIsOver(t *testing.T) {
	m := sized()
	m.run.waiting = false

	msg, ok := m.spin.Tick().(spinner.TickMsg)
	if !ok {
		t.Fatal("Tick did not produce a spinner.TickMsg")
	}
	if cmd := m.spun(msg); cmd != nil {
		t.Error("the spinner kept re-arming after the wait ended")
	}
}
