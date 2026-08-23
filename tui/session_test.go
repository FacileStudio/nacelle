package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/FacileStudio/nacelle"
)

// The number on screen has to be one a person can act on. Carrying a finished
// run's usage into the next one showed the old total plus the new turns, then
// dropped to the new run's total on its own — measured as 100, then 105, then
// 7, with the first run's hundred silently gone.
func TestTheTokenCountIsASessionTotalThatOnlyEverGrows(t *testing.T) {
	m := sized()
	m.agent = answering(t)

	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{OutputTokens: 40}})
	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{OutputTokens: 60}})
	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Usage: nacelle.Usage{OutputTokens: 100}})
	m.settle()
	if status := m.status(); !strings.Contains(status, "out 100") {
		t.Fatalf("status after the first run = %q, want its 100 output tokens", status)
	}

	m.prompt.SetValue("again please")
	m.ask()
	defer m.run.cancel()

	m.absorb(nacelle.Event{Kind: nacelle.KindTurn, Usage: nacelle.Usage{OutputTokens: 5}})
	if status := m.status(); !strings.Contains(status, "out 105") {
		t.Errorf("status during the second run = %q, want the session's 105 output tokens", status)
	}

	m.absorb(nacelle.Event{Kind: nacelle.KindDone, Usage: nacelle.Usage{OutputTokens: 7}})
	m.settle()
	if status := m.status(); !strings.Contains(status, "out 107") {
		t.Errorf("status after the second run = %q, want 100 + 7 rather than 7 output tokens", status)
	}
}

// Reading the failure before the half sentence it interrupted is the wrong
// order to read a failure in.
func TestAStreamErrorIsCommittedAfterTheAnswerItInterrupted(t *testing.T) {
	m := sized()
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "half an answer"})
	m.consume(result{err: errors.New("the stream fell over")})

	screen := onScreen(m)
	answer, failure := strings.Index(screen, "half an answer"), strings.Index(screen, "fell over")
	if answer < 0 || failure < 0 {
		t.Fatalf("screen = %q, want both the answer and the error on it", screen)
	}
	if failure < answer {
		t.Errorf("screen = %q, want the error under the text it interrupted", screen)
	}
}

// Reasoning written into the answer's buffer comes out concatenated: the last
// thought against the first word with no separator, and the whole run-on
// committed as the assistant's message, so every later turn re-sends a chain of
// thought the providers bill for and do not want replayed.
func TestReasoningIsShownApartAndKeptOutOfTheConversation(t *testing.T) {
	m := sized()
	m.absorb(nacelle.Event{Kind: nacelle.KindThinking, Text: "let me think"})
	m.absorb(nacelle.Event{Kind: nacelle.KindText, Text: "the answer"})
	m.settle()

	screen := onScreen(m)
	if !strings.Contains(screen, "let me think") || !strings.Contains(screen, "the answer") {
		t.Fatalf("screen = %q, want the reasoning and the answer both visible", screen)
	}
	if strings.Contains(screen, "thinkthe answer") {
		t.Errorf("screen = %q, want the reasoning not run into the first word", screen)
	}

	if len(m.conversation) != 1 || said(m.conversation[0]) != "the answer" {
		t.Errorf("conversation = %+v, want the answer alone", m.conversation)
	}
}

// The reasoning is shown as it streams, not held back until the run ends: a
// terminal that says nothing while the model thinks looks like one that hung.
func TestReasoningIsOnScreenWhileItIsStillStreaming(t *testing.T) {
	m := sized()
	m.run.busy = true
	m.absorb(nacelle.Event{Kind: nacelle.KindThinking, Text: "weighing it up"})

	if !strings.Contains(onScreen(m), "weighing it up") {
		t.Errorf("screen = %q, want the reasoning visible as it arrives", onScreen(m))
	}
}
