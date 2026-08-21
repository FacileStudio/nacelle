package main

import (
	"fmt"
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

	if strings.Contains(onScreen(m), "waiting its turn") {
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

	_, cmd := m.Update(finished{})
	printed := printedBy(cmd)
	defer m.run.cancel()

	if len(m.run.queued) != 0 {
		t.Errorf("queued = %v, want it emptied once delivered", m.run.queued)
	}
	if !strings.Contains(printed, "the second question") {
		t.Errorf("printed = %q, want the delivered question echoed into the transcript", printed)
	}
}

// A queued "/help" is still a command. Feeding the queue straight to send
// would have made it a question typed at the model instead.
func TestAQueuedCommandIsStillACommand(t *testing.T) {
	m := busy(t)
	m.prompt.SetValue("/help")
	m.ask()

	_, cmd := m.Update(finished{})
	printed := printedBy(cmd)
	defer m.run.cancel()

	if !strings.Contains(printed, "start a new session") {
		t.Errorf("printed = %q, want the delivered /help to have run as a command", printed)
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
	if !strings.Contains(onScreen(m), "dropped, not sent") {
		t.Errorf("screen = %q, want the drop reported rather than silent", onScreen(m))
	}
}

// Every row View draws below the transcript has to be reserved by layout, or
// the prompt is pushed off the bottom of the screen.
func TestQueuedMessagesTakeTheirRowsOutOfTheTranscript(t *testing.T) {
	m := busy(t)
	defer m.run.cancel()
	before := m.liveRows

	m.prompt.SetValue("one")
	m.ask()
	m.prompt.SetValue("two")
	m.ask()

	if got := m.liveRows; got != before-2 {
		t.Errorf("live rows = %d, want %d — one row reserved per queued message", got, before-2)
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
	tall := m.liveRows
	m.layout(m.windowHeight)

	if got := m.liveRows; got != tall {
		t.Errorf("live rows = %d, want it unchanged at %d with nothing queued", got, tall)
	}
}

// A queued command answers on the spot and starts no run, so a queue drained
// once per settle stranded everything behind it — still drawn as queued,
// still holding its row, and with nothing left that would ever send it.
func TestAQueuedCommandDoesNotStrandTheQuestionsBehindIt(t *testing.T) {
	m := busy(t)
	m.prompt.SetValue("/help")
	m.ask()
	m.prompt.SetValue("the question behind it")
	m.ask()

	_, cmd := m.Update(finished{})
	printed := printedBy(cmd)
	defer m.run.cancel()

	if len(m.run.queued) != 0 {
		t.Fatalf("queued = %v, want the queue drained past the command", m.run.queued)
	}
	if !strings.Contains(printed, "the question behind it") {
		t.Errorf("printed = %q, want the question after the command actually asked", printed)
	}
}

// The queue is a reminder of what is waiting, not a second transcript.
// Listing all of it made layout reserve a row per message, which squeezed the
// transcript to nothing and pushed the prompt off the bottom of the screen.
func TestALongQueueIsCountedRatherThanListedInFull(t *testing.T) {
	m := busy(t)
	defer m.run.cancel()
	for i := range 30 {
		m.prompt.SetValue(fmt.Sprintf("question %d", i))
		m.ask()
	}

	if got := len(m.viewQueued()); got != queuedRows+1 {
		t.Errorf("viewQueued drew %d rows, want %d listed plus one counting the rest", got, queuedRows+1)
	}
	if last := visible(m.viewQueued()[queuedRows]); !strings.Contains(last, "27 more") {
		t.Errorf("last row = %q, want it counting the %d not listed", last, 30-queuedRows)
	}
	if drawnRows(m) > m.windowHeight {
		t.Errorf("View drew %d rows into a %d-row window — the prompt is off-screen", drawnRows(m), m.windowHeight)
	}
}

// /clear returns a Cmd that prints, which is what made the queue's own drain
// an ordering problem: under tea.Batch two queued /clears can interleave their
// blank runs with each other's transcript, and a batch promises nothing about
// order. Asserting on the message type is the only seam — a sequence's own msg
// is unexported, so the check is that this is not a batch.
func TestDeliverSequencesQueuedCommandsRatherThanBatchingThem(t *testing.T) {
	m := sized()
	m.run.queued = []string{"/clear", "/clear"}

	cmd := m.deliver()

	if cmd == nil {
		t.Fatal("deliver returned nothing for two queued commands")
	}
	if _, batched := cmd().(tea.BatchMsg); batched {
		t.Error("deliver batched its commands, want them sequenced — /clear prints, and a batch makes no promise about order")
	}
}

// A queued "/clear" scrolls the screen away, so anything queued behind it has
// to be echoed after that run of blanks rather than before it. One drain at
// Update is what gets this wrong: deliver dispatches every queued line inside
// a single routed message, so a single flush collects all their echoes and
// prints the lot ahead of the clear that then scrolls them off.
func TestAQuestionQueuedBehindAClearIsEchoedBelowIt(t *testing.T) {
	m := busy(t)
	m.run.queued = []string{"/clear", "a later question"}

	_, cmd := m.Update(finished{})
	printed := printedBy(cmd)
	defer m.run.cancel()

	banner, question := strings.Index(printed, "cleared"), strings.Index(printed, "a later question")
	if question < 0 || banner < 0 {
		t.Fatalf("printed = %q, want the fresh banner and the question queued behind it", printed)
	}
	if question < banner {
		t.Errorf("printed = %q, want the question echoed below the clear rather than scrolled away by it", printed)
	}
}
