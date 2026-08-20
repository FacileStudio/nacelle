package main

import (
	"context"
	"iter"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
	"github.com/charmbracelet/x/ansi"
)

// visible is a rendered screen with the styling stripped, which is what a
// test checking for words rather than colours wants: markdown rendering wraps
// a paragraph in a colour escape per line, and a literal check for a phrase
// spanning two of those lines never finds it in the raw ANSI.
func visible(screen string) string { return ansi.Strip(screen) }

// sized is a model with a window, because everything that renders needs one.
func sized() *model {
	m := newModel(nil, "test · model", nil)
	m.resize(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// Text arrives a few characters at a time. A transcript with one entry per
// delta is unreadable, so they have to accumulate into a single answer.
func TestStreamedTextAccumulatesIntoOneAnswer(t *testing.T) {
	m := sized()
	for _, delta := range []string{"the ", "whole ", "answer"} {
		m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: delta})
	}

	if got := m.run.answer.String(); got != "the whole answer" {
		t.Errorf("answer = %q, want the deltas joined", got)
	}
	if lines := spoken(m); len(lines) != 0 {
		t.Errorf("transcript = %v, want the deltas kept out of it", lines)
	}
}

// A run that ends has to leave its answer in the conversation, or the next
// question is asked of a model that never said anything.
func TestAFinishedAnswerJoinsTheConversation(t *testing.T) {
	m := sized()
	m.run.busy = true
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "an answer"})
	m.settle()

	if len(m.conversation) != 1 {
		t.Fatalf("conversation = %v, want the answer appended", m.conversation)
	}
	if m.conversation[0].Role != nacelle.RoleAssistant || said(m.conversation[0]) != "an answer" {
		t.Errorf("message = %+v, want the assistant's answer", m.conversation[0])
	}
	if m.run.busy {
		t.Error("still busy after the run settled")
	}
	if m.run.answer.Len() != 0 {
		t.Error("the answer was left in the buffer for the next turn to repeat")
	}
}

// Watching a tool run is most of why a terminal is the first consumer.
func TestToolCallsAndResultsBothReachTheTranscript(t *testing.T) {
	m := sized()
	m.absorb(nacelle.Event{
		Kind: nacelle.KindToolCall,
		Tool: &nacelle.ToolEvent{Name: "read_file", Input: `{"path":"go.mod"}`},
	})
	m.absorb(nacelle.Event{
		Kind: nacelle.KindToolResult,
		Tool: &nacelle.ToolEvent{Name: "read_file", Duration: 3 * time.Millisecond},
	})

	lines := spoken(m)
	if len(lines) != 2 {
		t.Fatalf("transcript = %v, want the call and the result", lines)
	}
	if !strings.Contains(lines[0], "read_file") {
		t.Errorf("call line = %q, want the tool named", lines[0])
	}
	if !strings.Contains(lines[1], "3ms") {
		t.Errorf("result line = %q, want how long it took", lines[1])
	}
}

// KindDone carries the run's total, not another turn to add on. Adding it
// would bill every run for its last turn twice.
func TestTheRunTotalReplacesTheRunningCount(t *testing.T) {
	m := sized()
	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{InputTokens: 10}})
	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{InputTokens: 10}})
	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Usage: nacelle.Usage{InputTokens: 20}})

	if got := m.run.usage.Total(); got != 20 {
		t.Errorf("total = %d, want the run total rather than the sum of both", got)
	}
}

// The terminal is in raw mode, so nothing quits on Ctrl+C unless the model
// says so — and while an answer is streaming it should stop that answer rather
// than throwing the session away.
func TestCtrlCStopsTheRunBeforeItQuits(t *testing.T) {
	press := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	if press.String() != "ctrl+c" {
		t.Fatalf("constructed key = %q, want ctrl+c", press.String())
	}

	m := sized()
	stopped := false
	m.run.cancel, m.run.busy = func() { stopped = true }, true

	handled, cmd := m.key(press)
	if !handled {
		t.Fatal("ctrl+c was passed through to the prompt")
	}
	if !stopped {
		t.Error("the run was not cancelled")
	}
	if cmd != nil {
		t.Error("the program quit instead of stopping the run")
	}

	m.run.busy = false
	if _, cmd = m.key(press); cmd == nil {
		t.Error("ctrl+c with nothing running did not quit")
	}
}

// The answer streams into a buffer that render only draws while it is filling.
// Clearing that buffer without moving the text somewhere permanent erases the
// answer from the screen at the moment it finished, which is exactly what a
// first real session found.
func TestAFinishedAnswerStaysOnScreen(t *testing.T) {
	m := sized()
	m.run.busy = true
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "the whole answer"})
	m.settle()

	if !strings.Contains(onScreen(m), "the whole answer") {
		t.Errorf("screen = %q, want the finished answer still visible", onScreen(m))
	}
}

// The banner names what is about to be billed, before anything is typed.
func TestTheBackendAndModelAreShownBeforeTheFirstQuestion(t *testing.T) {
	m := sized()

	if !strings.Contains(onScreen(m), "test · model") {
		t.Errorf("screen = %q, want the backend and model named", onScreen(m))
	}
}

// A run cut off by the token ceiling arrives as a well-formed stream that
// simply ends, so the only thing separating a truncated answer from a finished
// one on screen is this line. "ready" under half a sentence is a lie.
func TestAnIncompleteRunSaysSoInTheStatusLine(t *testing.T) {
	incomplete := map[nacelle.Stop]string{
		nacelle.StopMaxTokens:  "token limit",
		nacelle.StopContext:    "out of context",
		nacelle.StopRefusal:    "refused",
		nacelle.StopIterations: "iteration limit",
		nacelle.StopOther:      "stopped early",
	}

	for stop, want := range incomplete {
		m := sized()
		m.absorb(nacelle.Event{Kind: nacelle.KindDone, Stop: stop})

		status := m.status()
		if !strings.Contains(status, want) {
			t.Errorf("status for %q = %q, want it to mention %q", stop, status, want)
		}
		if strings.Contains(status, "ready") {
			t.Errorf("status for %q = %q, want a truncated run not to read as a finished one", stop, status)
		}
	}
}

// The other half of the same bargain: a run that finished must not be dressed
// up as a problem, or the warning stops meaning anything.
func TestAFinishedRunLeavesTheStatusLineAlone(t *testing.T) {
	m := sized()
	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopEnd})

	if status := m.status(); !strings.Contains(status, "ready") {
		t.Errorf("status = %q, want the usual ready line", status)
	}
}

// The warning belongs to the run that earned it. Left standing, it would
// accuse the next answer of being truncated too.
func TestANewQuestionClearsTheStopFromTheLastRun(t *testing.T) {
	m := sized()
	m.agent = answering(t)
	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopMaxTokens})
	m.prompt.SetValue("again please")
	m.ask()
	defer m.run.cancel()

	if m.run.stop != "" {
		t.Errorf("stop = %q, want the previous run's reason cleared", m.run.stop)
	}
}

// silent is a backend that ends a run without saying anything, which is all a
// test that only cares about what asking does to the model needs.
type silent struct{}

func (silent) Name() string                       { return "silent" }
func (silent) Capabilities() nacelle.Capabilities { return nacelle.Capabilities{} }

func (silent) CountTokens(context.Context, nacelle.Request) (int64, error) { return 0, nil }

func (silent) Stream(context.Context, nacelle.Request) iter.Seq2[nacelle.Event, error] {
	return func(yield func(nacelle.Event, error) bool) {
		yield(nacelle.Event{Kind: nacelle.KindDone, Stop: nacelle.StopEnd}, nil)
	}
}

// answering is an agent that can be asked something. The model dereferences it
// as soon as a question is sent, so a test that calls ask needs a real one.
func answering(t *testing.T) *nacelle.Agent {
	t.Helper()

	agent, err := nacelle.New(nacelle.Config{Backend: silent{}, System: "be quiet"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return agent
}
