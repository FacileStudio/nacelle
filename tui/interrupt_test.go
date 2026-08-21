package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// stuck is a model in the state the escape hatch exists for: a run in flight
// whose cancellation goes nowhere, which is what a tool wedged on a subprocess
// leaves behind. busy is cleared by settle, settle waits for the results
// channel, and that channel never closes.
func stuck() *model {
	m := sized()
	m.run.cancel, m.run.busy = func() {}, true
	return m
}

// The only way out of an alt-screen raw-mode terminal whose ctrl+c is spent
// cancelling a run that cannot hear it is kill -9 from another terminal. A
// second press has to quit outright.
func TestASecondCtrlCQuitsARunThatWillNotStop(t *testing.T) {
	press := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	m := stuck()

	if handled, cmd := m.key(press); !handled || cmd != nil {
		t.Fatalf("first ctrl+c = %v, %v; want the run cancelled and the session kept", handled, cmd)
	}
	if _, cmd := m.key(press); cmd == nil {
		t.Fatal("a second ctrl+c did not quit, so the only way out is kill -9")
	}
}

// An escape hatch nobody knows about is not an escape hatch.
func TestTheStatusLineOffersTheEscapeAfterTheFirstCtrlC(t *testing.T) {
	m := stuck()

	if status := m.status(); strings.Contains(status, "ctrl+c") {
		t.Fatalf("status = %q, want no offer before anything was pressed", status)
	}

	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	status := m.status()
	if !strings.Contains(status, "ctrl+c") || !strings.Contains(status, "quit") {
		t.Errorf("status = %q, want it to say a second ctrl+c quits", status)
	}
}

// The second press is on a timer, so a client left running all afternoon must
// not have a stale ctrl+\ as its only remaining brake.
func TestCtrlBackslashQuitsWhateverTheRunIsDoing(t *testing.T) {
	press := tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl}
	if press.String() != "ctrl+\\" {
		t.Fatalf("constructed key = %q, want ctrl+backslash", press.String())
	}

	m := stuck()
	handled, cmd := m.key(press)
	if !handled || cmd == nil {
		t.Errorf("ctrl+backslash = %v, %v; want an immediate quit", handled, cmd)
	}
}

// Cancelling is the one abandonment the reader caused themselves, and it was
// the one the status line did not report: the stop reason only ever arrives on
// a KindDone that cancelling is what prevents.
func TestAnAbandonedRunSaysSoRatherThanReadingAsReady(t *testing.T) {
	m := stuck()
	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m.settle()

	status := m.status()
	if !strings.Contains(status, "abandoned") {
		t.Errorf("status = %q, want the run reported as abandoned", status)
	}
	if strings.Contains(status, "ready") {
		t.Errorf("status = %q, want half an answer not to read as a finished one", status)
	}
}

// The warning belongs to the run that earned it, so the next question clears
// it along with every other per-run reason.
func TestANewQuestionClearsTheAbandonedMark(t *testing.T) {
	m := stuck()
	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m.settle()

	m.agent = answering(t)
	m.prompt.SetValue("again please")
	m.ask()
	defer m.run.cancel()

	if m.run.stop != nacelle.Stop("") {
		t.Errorf("stop = %q, want the previous run's reason cleared", m.run.stop)
	}
}

// The offer to quit belongs to the run that was interrupted. Left standing it
// would make the first ctrl+c of the next question throw the session away,
// which is the opposite of what ctrl+c is for while an answer is streaming.
func TestAFreshQuestionTakesTheForceQuitOfferBackDown(t *testing.T) {
	m := stuck()
	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m.settle()

	m.agent = answering(t)
	m.prompt.SetValue("again please")
	m.ask()
	defer m.run.cancel()

	if handled, cmd := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !handled || cmd != nil {
		t.Errorf("ctrl+c = %v, %v; want the new run stopped rather than the client quit", handled, cmd)
	}
	if status := m.status(); !strings.Contains(status, "ctrl+c") {
		t.Errorf("status = %q, want the offer made afresh for this run", status)
	}
}

// The key that stops the answer must never be the key that might close the
// client, or every press has to be thought about first.
func TestEscapeStopsTheRunAndLeavesTheClientStanding(t *testing.T) {
	m := stuck()

	handled, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !handled || cmd != nil {
		t.Fatalf("esc = %v, %v; want the run stopped and the session kept", handled, cmd)
	}
	if m.run.stop != abandoned {
		t.Errorf("stop = %q, want the run marked abandoned", m.run.stop)
	}
	if status := m.status(); !strings.Contains(status, "stopping") {
		t.Errorf("status = %q, want it to say the run is stopping rather than still running", status)
	}
}

// The offer to quit is on a three-second timer, so re-stamping it on every
// press is how an esc held down — or tapped at a tool that is not listening —
// leaves ctrl+c meaning "quit now" for as long as the tapping lasts.
func TestASecondEscapeDoesNotKeepTheForceQuitOfferAlive(t *testing.T) {
	m := stuck()
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	armed := m.run.interrupted

	handled, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if !handled || cmd != nil {
		t.Errorf("second esc = %v, %v; want the press still claimed by the run", handled, cmd)
	}
	if !m.run.interrupted.Equal(armed) {
		t.Error("a second esc re-armed the offer, so holding it holds ctrl+c at quit")
	}
}

// Idle, esc is not this client's key at all. Claiming it would make the one
// press every terminal reader uses to back out of something a press that
// silently does nothing here, and stop it reaching the prompt.
func TestEscapeWithNothingRunningBelongsToThePrompt(t *testing.T) {
	m := sized()

	if handled, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape}); handled || cmd != nil {
		t.Errorf("esc while idle = %v, %v; want it left to the prompt", handled, cmd)
	}
}

// A dropdown standing open is the nearer thing to back out of, and it is what
// esc already meant there. Stopping the run out from under an open menu would
// take two visible things away on one press.
func TestEscapeClosesTheDropdownBeforeItStopsAnything(t *testing.T) {
	m := stuck()
	m.prompt.SetValue("/")
	m.refreshMenu()
	if !m.menu.open() {
		t.Fatal("the dropdown did not open, so there is nothing for esc to close first")
	}

	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.menu.open() {
		t.Error("the menu stayed open after esc")
	}
	if m.run.stop == abandoned {
		t.Error("esc stopped the run as well as closing the menu, want one thing per press")
	}
}
