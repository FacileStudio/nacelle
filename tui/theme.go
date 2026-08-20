package main

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

// palette is every style the transcript uses, resolved for the terminal's own
// background rather than guessed at.
//
// Guessing is the usual way a terminal client ends up unreadable: a palette
// picked on somebody's dark terminal is grey on grey for everyone whose
// terminal is light. Bubble Tea can ask the terminal what colour it is and the
// answer arrives as a message, so the palette is rebuilt when it does and the
// dark one is only the assumption held until then.
type palette struct {
	question lipgloss.Style
	thinking lipgloss.Style
	tool     lipgloss.Style
	result   lipgloss.Style
	failure  lipgloss.Style
	client   lipgloss.Style
	plain    lipgloss.Style
	waiting  lipgloss.Style
	markdown string
}

// themed builds the palette for a light or a dark terminal.
//
// The question is the only thing given a background, and it is a quiet one: it
// exists so the reader can find where they asked something while scrolling back
// through an answer, not to decorate. Everything that is not the answer is
// dimmed instead, because the answer is what the window is for.
func themed(dark bool) palette {
	pick := lipgloss.LightDark(dark)
	quiet := pick(lipgloss.Color("#6C6C6C"), lipgloss.Color("#909090"))

	style := "light"
	if dark {
		style = "dark"
	}

	return palette{
		question: lipgloss.NewStyle().
			Background(pick(lipgloss.Color("#E8E8EC"), lipgloss.Color("#26262C"))).
			Foreground(pick(lipgloss.Color("#1C1C1C"), lipgloss.Color("#E4E4E4"))).
			Padding(0, 1),
		thinking: lipgloss.NewStyle().Foreground(quiet).Italic(true).PaddingLeft(1),
		tool:     lipgloss.NewStyle().Foreground(pick(lipgloss.Color("#3A6EA5"), lipgloss.Color("#7AA2D4"))),
		result:   lipgloss.NewStyle().Foreground(quiet),
		failure:  lipgloss.NewStyle().Foreground(pick(lipgloss.Color("#B3261E"), lipgloss.Color("#F2827F"))),
		client:   lipgloss.NewStyle().Foreground(quiet),
		plain:    lipgloss.NewStyle(),
		waiting:  lipgloss.NewStyle().Foreground(quiet),
		markdown: style,
	}
}

// restyle rebuilds everything that depends on the width or the terminal's
// colour, and draws the transcript again with it.
func (m *model) restyle() {
	m.pretty = prettier(m.theme.markdown, max(m.width, 1))
}

// prettier builds the markdown renderer for one width and one terminal, or nil
// when it cannot be built. Nothing about an answer is worth failing to start
// over, and markdown renders as itself when it is not rendered at all.
func prettier(style string, width int) *glamour.TermRenderer {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	return renderer
}

// markdown renders an answer the way the model wrote it.
//
// Models answer in markdown whether or not anybody asked them to, so a terminal
// printing it raw shows the reader asterisks and backticks wrapped around the
// thing they wanted to read. Text the renderer cannot parse falls back to
// itself: an unreadable answer is worse than an unstyled one, and the model is
// perfectly capable of emitting markdown that is not quite markdown.
//
// The trailing newlines go because the renderer pads a document to sit alone on
// a page, and this one sits in a transcript that spaces its own entries.
func (m *model) markdown(text string) string {
	if m.pretty == nil {
		return text
	}
	rendered, err := m.pretty.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(rendered, "\n")
}
