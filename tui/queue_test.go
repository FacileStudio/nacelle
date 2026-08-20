package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// busy is a model with a run in flight, which is the only state in which
// queueing means anything.
func busy(t *testing.T) *model {
	t.Helper()

	m := sized()
	m.agent = answering(t)
	m.prompt.SetValue("the first question")
	m.ask()
	if !m.run.busy {
		t.Fatal("the run did not start, so nothing can be queued behind it")
	}
	return m
}

// Enter used to be silently ignored while a run was going. Nothing was said
// and nothing was sent, so the only way to find out the question had not been
// asked was to notice that nothing happened.
func TestTypingDuringARunQueuesInsteadOfBeingDropped(t *testing.T) {
	m := busy(t)
	defer m.run.cancel()

	m.prompt.SetValue("and then this one")
	m.ask()

	if got := m.run.queued; len(got) != 1 || got[0] != "and then this one" {
		t.Fatalf("queued = %v, want the line typed during the run", got)
	}
	if m.prompt.Value() != "" {
		t.Errorf("prompt = %q, want it cleared once the line was taken", m.prompt.Value())
	}
}

// A queued line is not in the transcript, because a transcript is what was
// actually said — parked above a half-finished answer it would read as
// already sent.
func TestAQueuedMessageIsShownWithoutJoiningTheTranscript(t *testing.T) {
	m := busy(t)
	defer m.run.cancel()

	m.prompt.SetValue("waiting its turn")
	m.ask()

	if strings.Contains(visible(m.viewport.View()), "waiting its turn") {
		t.Error("a queued line reached the transcript, where it reads as already asked")
	}
	if !strings.Contains(visible(strings.Join(m.viewQueued(), "\n")), "waiting its turn") {
		t.Errorf("viewQueued = %v, want the queued line shown somewhere", m.viewQueued())
	}
}

// The point of queueing: it is actually delivered, not merely remembered.
func TestSettleDeliversTheQueuedMessage(t *testing.T) {
	m := busy(t)
	m.prompt.SetValue("the second question")
	m.ask()

	m.settle()
	defer m.run.cancel()

	if len(m.run.queued) != 0 {
		t.Errorf("queued = %v, want it emptied once delivered", m.run.queued)
	}
	if !strings.Contains(visible(m.viewport.View()), "the second question") {
		t.Errorf("viewport = %q, want the delivered question echoed into the transcript", m.viewport.View())
	}
}

// A queued "/help" is still a command. Feeding the queue straight to send
// would have made it a question typed at the model instead.
func TestAQueuedCommandIsStillACommand(t *testing.T) {
	m := busy(t)
	m.prompt.SetValue("/help")
	m.ask()

	m.settle()
	defer m.run.cancel()

	if !strings.Contains(visible(m.viewport.View()), "start a new session") {
		t.Errorf("viewport = %q, want the delivered /help to have run as a command", m.viewport.View())
	}
	if m.run.busy {
		t.Error("a delivered /help started a run, so it was sent to the model as text")
	}
}

// Stopping a run has to stop what the run would have led to. Left alone the
// queue is delivered by settle, which cancelling reaches like any other
// ending — so ctrl+c would abandon one run and immediately start the next.
func TestCancellingDropsTheQueueRatherThanStartingIt(t *testing.T) {
	m := busy(t)
	defer m.run.cancel()
	m.prompt.SetValue("do not send me")
	m.ask()

	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if len(m.run.queued) != 0 {
		t.Fatalf("queued = %v, want it dropped when the run was cancelled", m.run.queued)
	}
	if !strings.Contains(visible(m.viewport.View()), "dropped, not sent") {
		t.Errorf("viewport = %q, want the drop reported rather than silent", m.viewport.View())
	}
}

// Every row View draws below the transcript has to be reserved by layout, or
// the prompt is pushed off the bottom of the screen.
func TestQueuedMessagesTakeTheirRowsOutOfTheTranscript(t *testing.T) {
	m := busy(t)
	defer m.run.cancel()
	before := m.viewport.Height()

	m.prompt.SetValue("one")
	m.ask()
	m.prompt.SetValue("two")
	m.ask()

	if got := m.viewport.Height(); got != before-2 {
		t.Errorf("viewport height = %d, want %d — one row reserved per queued message", got, before-2)
	}
}

// A queued message is only ever shown for a run that is still going, so an
// empty queue must cost the transcript nothing.
func TestAnEmptyQueueDrawsNothingAndCostsNoRows(t *testing.T) {
	m := sized()
	if lines := m.viewQueued(); len(lines) != 0 {
		t.Errorf("viewQueued = %v, want nothing drawn with an empty queue", lines)
	}

	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "unrelated"})
	tall := m.viewport.Height()
	m.layout(m.windowHeight)

	if got := m.viewport.Height(); got != tall {
		t.Errorf("viewport height = %d, want it unchanged at %d with nothing queued", got, tall)
	}
}
