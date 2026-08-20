package main

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// Models answer in markdown whether or not anybody asked them to. A terminal
// that prints it raw shows the reader asterisks and backticks instead of the
// emphasis and code they were meant to be.
func TestAnAnswerIsRenderedAsMarkdownRatherThanShownRaw(t *testing.T) {
	m := sized()
	m.say(fromModel, "this is **bold** and this is `code`")

	drawn := m.unprinted[len(m.unprinted)-1]
	if strings.Contains(drawn, "**") || strings.Contains(ansi.Strip(drawn), "**") {
		t.Errorf("drawn = %q, want the markdown syntax gone, not printed literally", drawn)
	}
	if !strings.Contains(ansi.Strip(drawn), "bold") || !strings.Contains(ansi.Strip(drawn), "code") {
		t.Errorf("drawn = %q, want the words still there once the syntax is stripped", drawn)
	}
}

// Nobody on screen is labelled — "you:" or "nacelle:" spends the margin on
// something the styling already carries. The question is found by its
// background instead, which is why it is the one entry that gets one.
func TestOnlyTheReaderSQuestionCarriesABackground(t *testing.T) {
	m := sized()
	m.say(fromReader, "what is in go.mod?")
	m.say(fromModel, "the module declaration")

	question := m.unprinted[len(m.unprinted)-2]
	answer := m.unprinted[len(m.unprinted)-1]

	if !strings.Contains(question, "48;2;") {
		t.Errorf("question = %q, want a background so it is findable while scrolling back", question)
	}
	if strings.Contains(answer, "48;2;") {
		t.Errorf("answer = %q, want no background — the answer is what the window is for", answer)
	}
	if strings.Contains(question, "you:") || strings.Contains(answer, "nacelle:") {
		t.Error("an entry was labelled with who said it, which the styling already says")
	}
}

// A palette picked for a dark terminal is grey on grey on a light one, so the
// client asks rather than guesses.
//
// It changes what is said from then on, not what was said before. Lines
// already printed belong to the terminal's scrollback and cannot be repainted
// — the same reason a resize no longer reflows them, and the trade the
// inline client makes to hand scrolling back to the terminal.
func TestTheTerminalSBackgroundPicksThePalette(t *testing.T) {
	m := sized()
	m.say(fromReader, "one question")
	before := m.unprinted[len(m.unprinted)-1]

	m.Update(tea.BackgroundColorMsg{Color: color.White})
	m.say(fromReader, "one question")
	after := m.unprinted[len(m.unprinted)-1]

	if before == after {
		t.Error("the palette did not change once the terminal reported a light background")
	}
	if m.theme.markdown != "light" {
		t.Errorf("markdown style = %q, want light for a light terminal", m.theme.markdown)
	}
}
