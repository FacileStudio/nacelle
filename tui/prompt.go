package main

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
)

// promptRows is how tall the prompt is allowed to grow before it scrolls
// inside itself instead.
//
// The prompt wraps and grows rather than scrolling sideways, which is what a
// single-line input did: a question longer than the terminal is wide slid out
// of view a character at a time and appeared to type over itself. Growing has
// to stop somewhere, though — every row it takes is a row of transcript, and
// a prompt free to fill the window would answer one complaint by creating a
// worse one.
const promptRows = 10

// newPrompt builds the question box.
//
// It is a textarea rather than a single-line input for one reason, and it is
// the whole feature: a text input scrolls sideways and a textarea wraps.
// DynamicHeight is what makes it grow, and it counts visual rows, so a long
// question with no newlines in it still earns the rows it needs.
//
// Enter is not bound here. It reaches key() first and sends, which is what
// this client has always done and what every peer harness does — so the
// newline that a textarea would normally spend enter on moves to alt+enter,
// with ctrl+j alongside it for terminals that will not report the first.
//
// The cursor is the terminal's own, not a drawn one, so the placeholder and
// the real caret never disagree about where typing will land.
func newPrompt() textarea.Model {
	prompt := textarea.New()
	prompt.Placeholder = "Ask something. Ctrl+C to stop or quit, Ctrl+\\ to force it."
	prompt.SetPromptFunc(2, continuation)
	prompt.ShowLineNumbers = false
	prompt.DynamicHeight = true
	prompt.MinHeight = 1
	prompt.MaxHeight = promptRows
	prompt.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	prompt.SetVirtualCursor(false)
	prompt.Focus()
	return prompt
}

// continuation is the marker down the left of the prompt: one "> " against
// the first row and blank against every row a long question wrapped onto.
//
// A prompt string is drawn once per row, so the plain one repeats itself all
// the way down and reads as several questions stacked up rather than as one
// that did not fit. The width is fixed at two either way, so the text still
// lines up under itself.
func continuation(info textarea.PromptInfo) string {
	if info.LineNumber == 0 {
		return "> "
	}
	return "  "
}
