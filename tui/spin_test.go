package main

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/FacileStudio/nacelle"
)

// A request can sit a full second or more before its first token, and a
// screen that has not moved since the question was echoed reads exactly like
// a client that stopped responding.
func TestAskingShowsTheSpinnerBeforeAnythingArrives(t *testing.T) {
	m := sized()
	m.agent = answering(t)
	m.prompt.SetValue("are you there?")
	m.ask()
	defer m.run.cancel()

	if !strings.Contains(visible(m.status()), "waiting for a response") {
		t.Errorf("status = %q, want it saying so while nothing has arrived yet", visible(m.status()))
	}
}

// The reported bug, and the reason the spinner moved out of the transcript.
// It used to stop at the first event of any kind, so every gap after that one
// — a tool running, the model called again with its result — was a still
// screen indistinguishable from a client that had died.
func TestTheSpinnerSurvivesTheFirstEvent(t *testing.T) {
	m := sized()
	m.agent = answering(t)
	m.prompt.SetValue("go on then")
	m.ask()
	defer m.run.cancel()

	m.consume(result{event: nacelle.Event{Kind: nacelle.KindText, Text: "h"}})

	if !m.run.busy {
		t.Fatal("the run stopped being busy on its first event, so this proves nothing")
	}
	msg := m.spin.Tick().(spinner.TickMsg)
	if cmd := m.spun(msg); cmd == nil {
		t.Error("the spinner stopped re-arming after the first event, while the run was still going")
	}
}

// Knowing something is happening is not the same as knowing what. A slow
// build under run_command and a wedged client look identical otherwise, and
// this client's own command timeout is measured in minutes.
func TestTheStatusLineNamesTheToolThatIsRunning(t *testing.T) {
	m := sized()
	m.run.busy = true

	m.absorb(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{ID: "a", Name: "read_file"}})

	if status := visible(m.status()); !strings.Contains(status, "running read_file") {
		t.Errorf("status = %q, want it naming the tool between its call and its result", status)
	}
}

// The SDK's own runner executes a turn's tools concurrently, so counting them
// by call id is what keeps the count honest when several are in flight.
func TestTheStatusLineCountsSeveralToolsAtOnce(t *testing.T) {
	m := sized()
	m.run.busy = true

	m.absorb(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{ID: "a", Name: "read_file"}})
	m.absorb(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{ID: "b", Name: "search_content"}})

	if status := visible(m.status()); !strings.Contains(status, "running 2 tools") {
		t.Errorf("status = %q, want it counting both calls in flight", status)
	}
}

// A tool that has answered is not still running, and a status line that says
// it is would be worse than one that said nothing.
func TestAToolStopsBeingNamedOnceItsResultArrives(t *testing.T) {
	m := sized()
	m.run.busy = true

	m.absorb(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{ID: "a", Name: "read_file"}})
	m.absorb(nacelle.Event{Kind: nacelle.KindToolResult, Tool: &nacelle.ToolEvent{ID: "a", Name: "read_file"}})

	if status := visible(m.status()); strings.Contains(status, "running read_file") {
		t.Errorf("status = %q, want the finished tool no longer named as running", status)
	}
}

// A run capped at its iteration limit reports the tools it stopped short of
// and runs none of them, and an abandoned one never reaches their results at
// all — so the set has to be emptied rather than trusted to drain.
func TestSettleForgetsToolsThatNeverAnswered(t *testing.T) {
	m := sized()
	m.run.cancel = func() {}
	m.run.busy = true
	m.absorb(nacelle.Event{Kind: nacelle.KindToolCall, Tool: &nacelle.ToolEvent{ID: "a", Name: "read_file"}})

	m.settle()

	if len(m.run.running) != 0 {
		t.Errorf("running = %v, want nothing left named as running after the run ended", m.run.running)
	}
}

// The library's Update always hands back a Cmd that re-arms the next frame.
// Not returning it is the only way the loop stops, so this is the one place
// that behaviour is worth locking down directly.
func TestTheSpinnerKeepsTickingWhileTheRunIsBusy(t *testing.T) {
	m := sized()
	m.run.busy = true

	msg, ok := m.spin.Tick().(spinner.TickMsg)
	if !ok {
		t.Fatal("Tick did not produce a spinner.TickMsg")
	}
	if cmd := m.spun(msg); cmd == nil {
		t.Fatal("the spinner did not re-arm its next tick while the run was still going")
	}
}

func TestTheSpinnerStopsTickingOnceTheRunIsOver(t *testing.T) {
	m := sized()
	m.run.busy = false

	msg, ok := m.spin.Tick().(spinner.TickMsg)
	if !ok {
		t.Fatal("Tick did not produce a spinner.TickMsg")
	}
	if cmd := m.spun(msg); cmd != nil {
		t.Error("the spinner kept re-arming after the run ended")
	}
}
