package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/FacileStudio/nacelle"
)

// sized is a model with a window, because everything that renders needs one.
func sized() *model {
	m := newModel(nil)
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

	if got := m.answer.String(); got != "the whole answer" {
		t.Errorf("answer = %q, want the deltas joined", got)
	}
	if len(m.transcript) != 0 {
		t.Errorf("transcript = %v, want the deltas kept out of it", m.transcript)
	}
}

// A run that ends has to leave its answer in the conversation, or the next
// question is asked of a model that never said anything.
func TestAFinishedAnswerJoinsTheConversation(t *testing.T) {
	m := sized()
	m.busy = true
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "an answer"})
	m.settle()

	if len(m.conversation) != 1 {
		t.Fatalf("conversation = %v, want the answer appended", m.conversation)
	}
	if !m.conversation[0].Assistant || m.conversation[0].Text != "an answer" {
		t.Errorf("message = %+v, want the assistant's answer", m.conversation[0])
	}
	if m.busy {
		t.Error("still busy after the run settled")
	}
	if m.answer.Len() != 0 {
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

	if len(m.transcript) != 2 {
		t.Fatalf("transcript = %v, want the call and the result", m.transcript)
	}
	if !strings.Contains(m.transcript[0], "read_file") {
		t.Errorf("call line = %q, want the tool named", m.transcript[0])
	}
	if !strings.Contains(m.transcript[1], "3ms") {
		t.Errorf("result line = %q, want how long it took", m.transcript[1])
	}
}

// KindDone carries the run's total, not another turn to add on. Adding it
// would bill every run for its last turn twice.
func TestTheRunTotalReplacesTheRunningCount(t *testing.T) {
	m := sized()
	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{InputTokens: 10}})
	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{InputTokens: 10}})
	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Usage: nacelle.Usage{InputTokens: 20}})

	if got := m.usage.Total(); got != 20 {
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
	m.cancel, m.busy = func() { stopped = true }, true

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

	m.busy = false
	if _, cmd = m.key(press); cmd == nil {
		t.Error("ctrl+c with nothing running did not quit")
	}
}
