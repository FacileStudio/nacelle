package main

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// long is a question wider than the 80 columns sized() gives the window, so
// it can only be shown by wrapping it.
const long = "this is a deliberately long question, far wider than the window it is " +
	"being typed into, so a prompt that refuses to wrap has nowhere to put it"

// The reported bug. A single-line input slid sideways as it filled, so a long
// question scrolled out of view a character at a time and looked like it was
// typing over itself.
func TestALongQuestionWrapsInsteadOfScrollingSideways(t *testing.T) {
	m := sized()
	m.prompt.SetValue(long)

	if got := m.prompt.Height(); got < 2 {
		t.Fatalf("prompt height = %d, want it grown past one row to fit %d characters", got, len(long))
	}
	if !strings.Contains(visible(m.prompt.View()), "this is a deliberately long question") {
		t.Errorf("prompt = %q, want the question actually shown", visible(m.prompt.View()))
	}
}

// Every row the prompt takes is a row of transcript. layout is the one place
// that arithmetic happens, so it has to ask the prompt rather than assume it
// is one row tall.
func TestAGrowingPromptTakesItsRowsFromTheTranscript(t *testing.T) {
	m := sized()
	before := m.viewport.Height()

	m.prompt.SetValue(long)
	m.layout(m.windowHeight)

	grew := m.prompt.Height() - 1
	if grew < 1 {
		t.Fatal("the prompt did not grow, so this proves nothing")
	}
	if got := m.viewport.Height(); got != before-grew {
		t.Errorf("viewport height = %d, want %d — one row given up per row the prompt gained", got, before-grew)
	}
}

// A prompt free to fill the window would answer one complaint by creating a
// worse one: a question being written must not cost the whole transcript.
func TestThePromptStopsGrowingAtItsCap(t *testing.T) {
	m := sized()
	m.prompt.SetValue(strings.Repeat("word ", 400))

	if got := m.prompt.Height(); got > promptRows {
		t.Errorf("prompt height = %d, want no more than the %d-row cap", got, promptRows)
	}
}

// Sending gives the rows back. Without this the transcript stays short after
// a long question was asked, for the rest of the session.
func TestSendingGivesThePromptsRowsBack(t *testing.T) {
	m := sized()
	m.agent = answering(t)
	tall := m.viewport.Height()

	m.prompt.SetValue(long)
	m.layout(m.windowHeight)
	m.ask()
	defer m.run.cancel()

	if got := m.viewport.Height(); got != tall {
		t.Errorf("viewport height = %d, want the full %d back once the prompt was cleared", got, tall)
	}
}

// A one-row prompt has no use for up and down, so they scroll. A taller one
// does: without them the cursor cannot reach the line above it.
func TestUpAndDownGoToThePromptOnlyWhileItIsTallerThanOneRow(t *testing.T) {
	m := filled()
	up := pressing(t, tea.KeyUp, "up")

	if !m.scroll(up) {
		t.Error("up did not scroll the transcript while the prompt was a single row")
	}

	m.prompt.SetValue(long)
	if !m.composing() {
		t.Fatal("the prompt did not grow, so this proves nothing")
	}
	if m.scroll(up) {
		t.Error("up scrolled the transcript instead of reaching a prompt with more than one line")
	}
}

// pgup and pgdown never move, so the transcript stays reachable by keyboard
// whatever the prompt is doing.
func TestPageKeysStayWithTheTranscriptWhileComposing(t *testing.T) {
	m := filled()
	m.prompt.SetValue(long)

	if !m.scroll(pressing(t, tea.KeyPgUp, "pgup")) {
		t.Error("pgup stopped scrolling the transcript once the prompt had grown")
	}
}

// A prompt string is drawn once per row, so repeating it down the left reads
// as several stacked questions rather than one that did not fit.
func TestOnlyTheFirstRowOfThePromptIsMarked(t *testing.T) {
	if got := continuation(promptInfo(0)); got != "> " {
		t.Errorf("first row marker = %q, want %q", got, "> ")
	}
	if got := continuation(promptInfo(1)); strings.TrimSpace(got) != "" {
		t.Errorf("wrapped row marker = %q, want it blank so the question reads as one", got)
	}
}

// promptInfo builds the argument the textarea hands a prompt function, so
// the test names the row it means rather than a bare struct literal.
func promptInfo(line int) textarea.PromptInfo {
	return textarea.PromptInfo{LineNumber: line, Focused: true}
}
