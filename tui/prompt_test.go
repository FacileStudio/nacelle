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

// Up and down now always belong to the prompt: there is no transcript in this
// client to scroll, so nothing competes for them and a wrapped question is
// editable at every height.
func TestUpAndDownReachThePrompt(t *testing.T) {
	m := sized()
	m.prompt.SetValue(long)

	if handled, _ := m.key(pressUp()); handled {
		t.Error("up was claimed by the client, want it left to the prompt")
	}
}

// pressUp is an up-arrow as bubbletea delivers it, checked against the
// library's own name so a rename fails here rather than silently unbinding.
func pressUp() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyUp}
}

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
	before := m.liveRows

	m.prompt.SetValue(long)
	m.layout(m.windowHeight)

	grew := m.prompt.Height() - 1
	if grew < 1 {
		t.Fatal("the prompt did not grow, so this proves nothing")
	}
	if got := m.liveRows; got != before-grew {
		t.Errorf("live rows = %d, want %d — one row given up per row the prompt gained", got, before-grew)
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
	tall := m.liveRows

	m.prompt.SetValue(long)
	m.layout(m.windowHeight)
	m.ask()
	defer m.run.cancel()

	if got := m.liveRows; got != tall {
		t.Errorf("live rows = %d, want the full %d back once the prompt was cleared", got, tall)
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

// drawnRows is how many rows View actually paints, which must never exceed
// the window it is painting into.
func drawnRows(m *model) int { return strings.Count(m.View().Content, "\n") + 1 }

// promptRows is a ceiling, not a promise: ten rows of prompt in an eight-row
// pane drew twelve rows into eight, and what fell off the bottom was the
// prompt, so the reader was typing into something they could not see.
func TestThePromptNeverOutgrowsASmallWindow(t *testing.T) {
	for _, height := range []int{6, 8, 12, 24} {
		m := newModel(nil, "test · model", nil)
		m.resize(tea.WindowSizeMsg{Width: 60, Height: height})
		m.prompt.SetValue(strings.Repeat("word ", 300))
		m.layout(m.windowHeight)

		if got := drawnRows(m); got > height {
			t.Errorf("window %d: View drew %d rows, want no more than the window holds", height, got)
		}
	}
}
