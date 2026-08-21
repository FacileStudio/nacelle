package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// Nothing is printed until Update drains the queue, because printing is a Cmd
// and say has callers that cannot return one.
func TestSayingSomethingQueuesItForTheTerminal(t *testing.T) {
	m := sized()
	m.say(fromReader, "a question")

	if said := spoken(m); len(said) != 1 || !strings.Contains(said[0], "a question") {
		t.Errorf("said = %v, want the one line waiting to be printed", said)
	}
}

// One Println for the batch, not one per line: tea.Batch promises nothing
// about the order its commands run in, and a transcript out of order is not a
// transcript. The body is read back through fmt because the message type is
// the library's own and unexported.
func TestEverythingSaidGoesOutAsOneMessageInOrder(t *testing.T) {
	m := sized()
	m.say(fromReader, "asked first")
	m.say(fromModel, "answered second")

	cmd := m.prints()
	if cmd == nil {
		t.Fatal("nothing was printed, want the queued lines handed to the terminal")
	}

	body := visible(fmt.Sprint(cmd()))
	first, second := strings.Index(body, "asked first"), strings.Index(body, "answered second")
	if first < 0 || second < 0 {
		t.Fatalf("printed = %q, want both lines in the one message", body)
	}
	if first > second {
		t.Errorf("printed = %q, want the question before the answer", body)
	}
}

// Draining has to empty the queue, or every message reprints everything said
// before it.
func TestPrintingForgetsWhatItHandedOver(t *testing.T) {
	m := sized()
	m.say(fromReader, "a question")
	m.prints()

	if len(m.unprinted) != 0 {
		t.Errorf("unprinted = %v, want it emptied once handed to the terminal", m.unprinted)
	}
	if m.prints() != nil {
		t.Error("a second drain produced a command, want nothing left to print")
	}
}

// The live region is repainted in place on every delta, so it has to fit on
// the screen. Content taller than the terminal cannot be redrawn where it
// stands, and an inline program that tries corrupts its own output.
func TestStreamingIsTailedToTheRowsTheWindowCanSpare(t *testing.T) {
	m := sized()
	m.liveRows = 3
	m.run.answer.WriteString(strings.Repeat("a line of streamed answer\n", 40))

	if got := len(m.streaming()); got > m.liveRows {
		t.Errorf("streaming drew %d rows, want no more than the %d it was given", got, m.liveRows)
	}
}

// What scrolls out of the live region is not lost: the whole answer is
// printed, rendered, the moment the run commits it.
func TestTheWholeAnswerIsPrintedEvenThoughOnlyItsTailWasShown(t *testing.T) {
	m := sized()
	m.liveRows = 2
	m.run.answer.WriteString("the opening line\nand many more\nand the last one")

	m.flush()

	if said := strings.Join(spoken(m), "\n"); !strings.Contains(said, "the opening line") {
		t.Errorf("said = %q, want the start of the answer printed, not only the visible tail", said)
	}
}

// A tool's call line announced a tool that had already finished. absorb says
// the line, consume returns the wait for the next event, and the next event
// after a call is that call's own result — so the line naming a six-second
// command reached the screen at the same moment the command did. Measured
// against OpenRouter: said at 1467ms, drawn at 7451ms.
// The closed channel is not scenery: reading what consume printed also runs
// the wait it re-armed, and a nil one never sends.
func TestAToolIsAnnouncedWhenItIsCalledNotWhenItReturns(t *testing.T) {
	m := sized()
	m.run.busy = true

	ended := make(chan result)
	close(ended)
	m.run.results = ended

	_, cmd := m.Update(result{event: nacelle.Event{
		Kind: nacelle.KindToolCall,
		Tool: &nacelle.ToolEvent{ID: "1", Name: "run_command"},
	}})

	if printed := printedBy(cmd); !strings.Contains(printed, "run_command") {
		t.Errorf("printed = %q, want the call announced before the wait for its result", printed)
	}
}

// The question has to reach the screen when it is asked, not when it is
// answered. waitFor blocks until the model's first token, a batch is not
// finished until every command in it is, and Update sequences the print queue
// behind whatever the routed message returned — so the echo sat there until
// the reply arrived. What that looked like was the prompt emptying and nothing
// else happening, which reads as a client that swallowed the question.
func TestTheQuestionIsPrintedWithoutWaitingOnTheAnswer(t *testing.T) {
	m := sized()
	m.agent = answering(t)
	m.prompt.SetValue("a question")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	printed := printedBy(cmd)
	defer m.run.cancel()

	if !strings.Contains(printed, "a question") {
		t.Errorf("printed = %q, want the question handed over before the run is waited on", printed)
	}
}
