package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/FacileStudio/nacelle"
)

// View draws the transcript, the prompt and one status line.
//
// The cursor is positioned by hand because the prompt renders inside a larger
// frame: the component reports where its cursor sits within itself, and only
// the caller knows how many rows are above it.
func (m *model) View() tea.View {
	transcript := m.viewport.View()
	body := strings.Join([]string{transcript, m.status(), m.prompt.View()}, "\n")

	view := tea.NewView(body)
	view.AltScreen = true
	if position := m.prompt.Cursor(); position != nil {
		position.Y += lipgloss.Height(transcript) + 1
		view.Cursor = position
	}
	return view
}

// status is the one line that is always true: what the run has cost so far,
// and whether it is still going.
//
// Cost is only shown when a backend reports one. Anthropic returns tokens and
// nothing else, and a zero next to a currency symbol reads as free rather than
// as unknown.
func (m *model) status() string {
	state := "ready"
	if m.busy {
		state = "working"
	}

	line := fmt.Sprintf("%s · %d tokens", state, m.usage.Total())
	if m.usage.CacheReadTokens > 0 {
		line += fmt.Sprintf(" (%d cached)", m.usage.CacheReadTokens)
	}
	if m.usage.Cost > 0 {
		line += fmt.Sprintf(" · $%.4f", m.usage.Cost)
	}
	return lipgloss.NewStyle().Faint(true).Render(line)
}

// absorb folds one event into what is on screen.
//
// Text accumulates into the answer being streamed rather than becoming a line
// of its own: the deltas arrive a few characters at a time, and a transcript
// of those is unreadable.
func (m *model) absorb(event nacelle.Event) {
	switch event.Kind {
	case nacelle.KindText:
		m.answer.WriteString(event.Text)
	case nacelle.KindThinking:
		m.answer.WriteString(event.Text)
	case nacelle.KindToolCall:
		m.say("tool", fmt.Sprintf("%s %s", event.Tool.Name, event.Tool.Input))
	case nacelle.KindToolResult:
		m.say("tool", describe(event.Tool))
	case nacelle.KindTurn:
		m.usage = m.usage.Add(event.Usage)
	case nacelle.KindDone:
		m.usage = event.Usage
	}
}

// describe is what a finished tool call reads as. A failure is reported rather
// than hidden, because the model is about to be told the same thing and the
// person watching should see what it sees.
func describe(tool *nacelle.ToolEvent) string {
	if tool.Err != nil {
		return fmt.Sprintf("%s failed after %s: %v", tool.Name, tool.Duration.Round(time.Millisecond), tool.Err)
	}
	return fmt.Sprintf("%s done in %s", tool.Name, tool.Duration.Round(time.Millisecond))
}

// say commits one labelled line to the transcript.
func (m *model) say(role, text string) {
	m.transcript = append(m.transcript, fmt.Sprintf("%s: %s", role, text))
	m.render()
}

// render redraws the transcript and pins it to the bottom. The viewport does
// not follow new content on its own, so appending without this leaves the
// stream scrolling out of sight.
func (m *model) render() {
	body := strings.Join(m.transcript, "\n\n")
	if streaming := m.answer.String(); streaming != "" {
		body += "\n\n" + streaming
	}
	m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width()).Render(body))
	m.viewport.GotoBottom()
}
